package duplocloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// DUPLOAI-3039: resource_grants on duploai_admin_permission_set. These tests
// drive the real embedded spec through the real provider (terraform CLI +
// providerserver) against an httptest backend that stores and echoes the
// PermissionSet record, so each hop is covered: config validators, request
// body (apiPath of every rule leaf), read-back, and the next plan.

// psStub is an in-memory PermissionSet backend. readTransform lets a test make
// the backend normalise the stored record the way the real one may.
type psStub struct {
	mu            sync.Mutex
	rec           map[string]any
	requests      []string // "METHOD path body"
	readTransform func(rec map[string]any) map[string]any
}

func newPSStub(t *testing.T, readTransform func(map[string]any) map[string]any) (*psStub, *httptest.Server) {
	s := &psStub{readTransform: readTransform}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests = append(s.requests, r.Method+" "+r.URL.Path+" "+string(b))
		if !strings.HasPrefix(r.URL.Path, "/v1/aiservicedesk/admin/data/PermissionSet") {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodPost, http.MethodPut:
			var in map[string]any
			_ = json.Unmarshal(b, &in)
			in["id"] = "ps-1"
			in["version"] = 1
			in["createdAt"] = "2026-11-01T10:00:00Z"
			in["updatedAt"] = "2026-11-01T10:00:00Z"
			s.rec = in
			s.write(w)
		case http.MethodGet:
			if s.rec == nil {
				http.NotFound(w, r)
				return
			}
			s.write(w)
		case http.MethodDelete:
			s.rec = nil
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	return s, srv
}

func (s *psStub) write(w http.ResponseWriter) {
	out := s.rec
	if s.readTransform != nil {
		cp := map[string]any{}
		raw, _ := json.Marshal(s.rec)
		_ = json.Unmarshal(raw, &cp)
		out = s.readTransform(cp)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
}

func (s *psStub) bodies(method string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, r := range s.requests {
		parts := strings.SplitN(r, " ", 3)
		if parts[0] != method {
			continue
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(parts[2]), &m)
		out = append(out, m)
	}
	return out
}

func psProvider(url string) string {
	return fmt.Sprintf("provider \"duploai\" {\n  duplo_host  = %q\n  duplo_token = \"tok\"\n}\n", url)
}

func psPath3() string {
	return `[
        { node_type = "Workspace",     node_id = "ws-1" },
        { node_type = "Environment",   node_id = "env-1" },
        { node_type = "ResourceGroup", node_id = "rg-1" },
      ]`
}

func psUnit(t *testing.T, srvURL string, steps ...resource.TestStep) {
	t.Helper()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    steps,
	})
}

func psPlanOnly(cfg string, errRe string) resource.TestStep {
	st := resource.TestStep{Config: cfg, PlanOnly: true, ExpectNonEmptyPlan: errRe == ""}
	if errRe != "" {
		st.ExpectError = regexp.MustCompile(errRe)
	}
	return st
}

func psGrantsCfg(url, allowVerbs, denyVerbs string) string {
	return psProvider(url) + fmt.Sprintf(`
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  resource_grants = {
    allow = [{
      path = %s
      verbs = %s
      allow_all_children = true
    }]
    deny = [{
      path = %s
      verbs = %s
    }]
  }
}`, psPath3(), allowVerbs, psPath3(), denyVerbs)
}

// --- hop 1: config validators ---

func TestPSGrants_BadVerbInAllowRuleFailsPlan(t *testing.T) {
	psUnit(t, "", psPlanOnly(psGrantsCfg("http://127.0.0.1:1", `["BOGUS"]`, `["GET"]`), `BOGUS|must be one of|Invalid Attribute Value`))
}

func TestPSGrants_BadVerbInDenyRuleFailsPlan(t *testing.T) {
	psUnit(t, "", psPlanOnly(psGrantsCfg("http://127.0.0.1:1", `["GET"]`, `["BOGUS"]`), `BOGUS|must be one of|Invalid Attribute Value`))
}

func TestPSGrants_LowercaseVerbRejected(t *testing.T) {
	psUnit(t, "", psPlanOnly(psGrantsCfg("http://127.0.0.1:1", `["get"]`, `["GET"]`), `must be one of|Invalid Attribute Value`))
}

func TestPSGrants_EmptyStringVerbRejected(t *testing.T) {
	psUnit(t, "", psPlanOnly(psGrantsCfg("http://127.0.0.1:1", `[""]`, `["GET"]`), `must be one of|Invalid Attribute Value`))
}

func TestPSGrants_WhitespaceVerbRejected(t *testing.T) {
	psUnit(t, "", psPlanOnly(psGrantsCfg("http://127.0.0.1:1", `[" GET "]`, `["GET"]`), `must be one of|Invalid Attribute Value`))
}

func TestPSGrants_ValidVerbsAccepted(t *testing.T) {
	psUnit(t, "", psPlanOnly(psGrantsCfg("http://127.0.0.1:1", `["GET", "ALL"]`, `["PUT", "POST", "DELETE"]`), ""))
}

func TestPSGrants_EmptyPathRejected(t *testing.T) {
	cfg := psProvider("http://127.0.0.1:1") + `
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  resource_grants = { allow = [{ path = [], verbs = ["GET"] }] }
}`
	psUnit(t, "", psPlanOnly(cfg, `(?i)at least 1|minimum|Invalid Attribute`))
}

func TestPSGrants_MissingPathRejected(t *testing.T) {
	cfg := psProvider("http://127.0.0.1:1") + `
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  resource_grants = { deny = [{ verbs = ["GET"] }] }
}`
	psUnit(t, "", psPlanOnly(cfg, `(?i)path|Inconsistent|Incorrect attribute value type`))
}

func TestPSGrants_NullVerbsAndEmptyVerbsLegal(t *testing.T) {
	cfg := psProvider("http://127.0.0.1:1") + fmt.Sprintf(`
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  resource_grants = {
    allow = [{ path = %s }]
    deny  = [{ path = %s, verbs = [] }]
  }
}`, psPath3(), psPath3())
	psUnit(t, "", psPlanOnly(cfg, ""))
}

func TestPSGrants_ConflictWithAllowedWorkspaces(t *testing.T) {
	cfg := psProvider("http://127.0.0.1:1") + fmt.Sprintf(`
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  allowed_workspaces = [{ workspace_id = "ws-1" }]
  resource_grants = { allow = [{ path = %s, verbs = ["GET"] }] }
}`, psPath3())
	psUnit(t, "", psPlanOnly(cfg, `Conflicting attributes|only one of`))
}

func TestPSGrants_LegacyScopesWithGrantsConflict(t *testing.T) {
	cfg := psProvider("http://127.0.0.1:1") + fmt.Sprintf(`
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  allowed_workspaces = [{ workspace_id = "ws-1", allowed_scopes = ["a.*"], denied_scopes = ["b.*"] }]
  resource_grants = { deny = [{ path = %s, verbs = ["GET"] }] }
}`, psPath3())
	psUnit(t, "", psPlanOnly(cfg, `Conflicting attributes|only one of`))
}

func TestPSGrants_OnlyLegacyOrOnlyGrantsOK(t *testing.T) {
	legacy := psProvider("http://127.0.0.1:1") + `
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  allowed_workspaces = [{ workspace_id = "ws-1" }]
}`
	psUnit(t, "", psPlanOnly(legacy, ""))
	// Migration step 1 is clearing the legacy list in its own apply.
	psUnit(t, "", psPlanOnly(psProvider("http://127.0.0.1:1")+`
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  allowed_workspaces = []
}`, ""))
}

// A reference unknown at plan time counts as set (conservative): documented.
func TestPSGrants_UnknownGrantsWithLegacyConflicts(t *testing.T) {
	cfg := psProvider("http://127.0.0.1:1") + `
resource "terraform_data" "x" { input = timestamp() }
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  allowed_workspaces = [{ workspace_id = "ws-1" }]
  resource_grants = { allow = [{ path = [{ node_type = "Workspace", node_id = terraform_data.x.output }], verbs = ["GET"] }] }
}`
	psUnit(t, "", psPlanOnly(cfg, `Conflicting attributes|only one of`))
}

// An unknown that resolves to null must not be rejected: the user set only one.
func TestPSGrants_UnknownResolvingToNullIsNotAConflict(t *testing.T) {
	cfg := psProvider("http://127.0.0.1:1") + fmt.Sprintf(`
resource "terraform_data" "x" { input = timestamp() }
locals { maybe = terraform_data.x.output == "never" ? [{ workspace_id = "ws-1" }] : null }
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  allowed_workspaces = local.maybe
  resource_grants = { allow = [{ path = %s, verbs = ["GET"] }] }
}`, psPath3())
	psUnit(t, "", psPlanOnly(cfg, ""))
}

// --- schema-level: deprecation, compat, oneOf load rule ---

func TestPSGrants_LegacyAttrsDeprecatedNotRequiredTypesUnchanged(t *testing.T) {
	spec := customerManagedSpecs(t)["admin_permission_set"]
	if spec == nil {
		t.Fatal("no admin_permission_set spec")
	}
	aw := spec.attr("allowed_workspaces")
	if aw == nil || aw.Type != "list(object)" || aw.Required || aw.Deprecated == "" {
		t.Fatalf("allowed_workspaces = %+v, want deprecated optional list(object)", aw)
	}
	for _, n := range []string{"allowed_scopes", "denied_scopes"} {
		var found *AttributeSpec
		for i := range aw.Attributes {
			if aw.Attributes[i].Name == n {
				found = &aw.Attributes[i]
			}
		}
		if found == nil || found.Deprecated == "" || found.Required {
			t.Errorf("%s = %+v, want deprecated and not required", n, found)
		}
		if found != nil && found.Type != "list(string)" && found.Type != "set(string)" {
			t.Errorf("%s type = %s, want unchanged string collection", n, found.Type)
		}
	}
	rg := spec.attr("resource_grants")
	if rg == nil || rg.Required || !rg.Optional || !rg.Computed || rg.Default != nil || rg.ForceNew {
		t.Fatalf("resource_grants = %+v, want optional+computed, no default, no forceNew", rg)
	}
}

func TestPSGrants_VerbsEnumMatchesAllowAndDenyAndIsSetOfString(t *testing.T) {
	spec := customerManagedSpecs(t)["admin_permission_set"]
	rg := spec.attr("resource_grants")
	want := []string{"ALL", "GET", "PUT", "POST", "DELETE"}
	for _, side := range rg.Attributes {
		for _, leaf := range side.Attributes {
			switch leaf.Name {
			case "verbs":
				if leaf.Type != "set(string)" || strings.Join(leaf.OneOf, ",") != strings.Join(want, ",") {
					t.Errorf("%s.verbs = %s oneOf %v", side.Name, leaf.Type, leaf.OneOf)
				}
			case "actions", "scopes":
				if leaf.Type != "set(string)" {
					t.Errorf("%s.%s type = %s, want set(string)", side.Name, leaf.Name, leaf.Type)
				}
			}
		}
	}
}

func TestPSGrants_OneOfOnNonStringCollectionFailsSpecLoad(t *testing.T) {
	for _, typ := range []string{"bool", "list(object)", "object", "list(int)", "map(object)"} {
		raw := fmt.Sprintf(`{"name":"probe","description":"p","idPath":"id","endpoint":{"uriBase":"/v1/x"},
		  "attributes":[{"name":"a","type":%q,"optional":true,"apiPath":"a","oneOf":["A"],
		  "attributes":[{"name":"b","type":"string","optional":true,"apiPath":"b"}]}]}`, typ)
		var spec ResourceSpec
		if err := json.Unmarshal([]byte(raw), &spec); err != nil {
			t.Fatal(err)
		}
		if err := spec.validate(); err == nil || !strings.Contains(err.Error(), "oneOf") {
			t.Errorf("oneOf on %s: err = %v, want a oneOf spec-load error", typ, err)
		}
	}
}

func TestPSGrants_NestedVerbsValidatorWired(t *testing.T) {
	spec := customerManagedSpecs(t)["admin_permission_set"]
	r := &dynamicResource{spec: *spec}
	var sr fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &sr)
	rg := sr.Schema.Attributes["resource_grants"].(schema.SingleNestedAttribute)
	for _, side := range []string{"allow", "deny"} {
		la := rg.Attributes[side].(schema.ListNestedAttribute)
		v := la.NestedObject.Attributes["verbs"].(schema.SetAttribute)
		if len(v.Validators) == 0 {
			t.Errorf("%s.verbs has no validators", side)
		}
		p := la.NestedObject.Attributes["path"].(schema.ListNestedAttribute)
		if len(p.Validators) == 0 {
			t.Errorf("%s.path has no minItems validator", side)
		}
	}
}

// --- hop 2: request body ---

func TestPSGrants_CreateBodyCarriesEveryRuleLeafAndNoActionsForAll(t *testing.T) {
	stub, srv := newPSStub(t, nil)
	cfg := psProvider(srv.URL) + fmt.Sprintf(`
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  resource_grants = {
    allow = [
      { path = %s, verbs = ["ALL"], allow_all_children = true, scopes = ["scope-9"] },
      { path = %s, verbs = ["ALL"], actions = ["aws.jit"] },
    ]
    deny = [{ path = %s, verbs = ["DELETE"] }]
  }
}`, psPath3(), psPath3(), psPath3())
	psUnit(t, srv.URL, resource.TestStep{Config: cfg, Check: func(*terraform.State) error {
		posts := stub.bodies("POST")
		if len(posts) != 1 {
			return fmt.Errorf("want 1 POST, got %d", len(posts))
		}
		rg, _ := posts[0]["resourceGrants"].(map[string]any)
		allow, _ := rg["allow"].([]any)
		deny, _ := rg["deny"].([]any)
		if len(allow) != 2 || len(deny) != 1 {
			return fmt.Errorf("allow=%d deny=%d body=%v", len(allow), len(deny), posts[0])
		}
		r0 := allow[0].(map[string]any)
		path, _ := r0["path"].([]any)
		if len(path) != 3 || path[0].(map[string]any)["nodeType"] != "Workspace" || path[2].(map[string]any)["nodeId"] != "rg-1" {
			return fmt.Errorf("path order/apiPaths wrong: %v", r0["path"])
		}
		if r0["allowAllChildren"] != true {
			return fmt.Errorf("allowAllChildren = %v", r0["allowAllChildren"])
		}
		if a, present := r0["actions"]; present {
			if l, _ := a.([]any); len(l) != 0 {
				return fmt.Errorf("verbs=[ALL] with no actions sent actions %v", a)
			}
		}
		if s, _ := r0["scopes"].([]any); len(s) != 1 || s[0] != "scope-9" {
			return fmt.Errorf("scopes = %v", r0["scopes"])
		}
		if v, _ := allow[1].(map[string]any)["actions"].([]any); len(v) != 1 || v[0] != "aws.jit" {
			return fmt.Errorf("explicit actions lost: %v", allow[1])
		}
		if dv, _ := deny[0].(map[string]any)["verbs"].([]any); len(dv) != 1 || dv[0] != "DELETE" {
			return fmt.Errorf("deny verbs = %v", deny[0])
		}
		return nil
	}})
}

// --- hops 3/4: read-back then next plan converges (UnitTest fails on a
// non-empty post-apply plan) ---

func psConverges(t *testing.T, transform func(map[string]any) map[string]any, cfgBody string, checks ...resource.TestCheckFunc) {
	t.Helper()
	_, srv := newPSStub(t, transform)
	psUnit(t, srv.URL, resource.TestStep{
		Config: psProvider(srv.URL) + "\nresource \"duploai_admin_permission_set\" \"t\" {\n  name = \"ps\"\n" + cfgBody + "\n}",
		Check:  resource.ComposeTestCheckFunc(checks...),
	})
}

func psRules(m map[string]any, side string) []any {
	rg, _ := m["resourceGrants"].(map[string]any)
	l, _ := rg[side].([]any)
	return l
}

// backend fills the cached node name on read: no drift.
func TestPSGrants_NameComputedRewriteNoDrift(t *testing.T) {
	fill := func(m map[string]any) map[string]any {
		for _, side := range []string{"allow", "deny"} {
			for _, r := range psRules(m, side) {
				for _, p := range r.(map[string]any)["path"].([]any) {
					pm := p.(map[string]any)
					pm["name"] = "name-of-" + fmt.Sprint(pm["nodeId"])
				}
			}
		}
		return m
	}
	psConverges(t, fill, fmt.Sprintf(`resource_grants = { allow = [{ path = %s, verbs = ["GET"] }] }`, psPath3()),
		resource.TestCheckResourceAttr("duploai_admin_permission_set.t", "resource_grants.allow.0.path.2.name", "name-of-rg-1"),
		resource.TestCheckResourceAttr("duploai_admin_permission_set.t", "resource_grants.allow.0.path.0.node_id", "ws-1"),
	)
}

func TestPSGrants_BackendOmitsFalseAllowAllChildren(t *testing.T) {
	drop := func(m map[string]any) map[string]any {
		for _, side := range []string{"allow", "deny"} {
			for _, r := range psRules(m, side) {
				rm := r.(map[string]any)
				if rm["allowAllChildren"] == false {
					delete(rm, "allowAllChildren")
				}
			}
		}
		return m
	}
	psConverges(t, drop, fmt.Sprintf(`resource_grants = { allow = [{ path = %s, verbs = ["GET"], allow_all_children = false }] }`, psPath3()),
		resource.TestCheckResourceAttr("duploai_admin_permission_set.t", "resource_grants.allow.0.allow_all_children", "false"),
	)
}

func TestPSGrants_AllowAllChildrenFalseKeptWhenBackendReturnsIt(t *testing.T) {
	psConverges(t, nil, fmt.Sprintf(`resource_grants = { allow = [{ path = %s, verbs = ["GET"], allow_all_children = false }] }`, psPath3()),
		resource.TestCheckResourceAttr("duploai_admin_permission_set.t", "resource_grants.allow.0.allow_all_children", "false"),
	)
}

func TestPSGrants_OmittedAllowOrDenyListConverges(t *testing.T) {
	psConverges(t, nil, fmt.Sprintf(`resource_grants = { deny = [{ path = %s, verbs = ["DELETE"] }] }`, psPath3()))
	psConverges(t, nil, fmt.Sprintf(`resource_grants = { allow = [{ path = %s, verbs = ["GET"] }] }`, psPath3()))
}

// Backend returns null for an explicit empty list, and [] for an omitted one.
func TestPSGrants_NullVsEmptyListsConverge(t *testing.T) {
	dropEmpty := func(m map[string]any) map[string]any {
		if rg, ok := m["resourceGrants"].(map[string]any); ok {
			for _, side := range []string{"allow", "deny"} {
				if l, ok := rg[side].([]any); ok && len(l) == 0 {
					delete(rg, side)
				}
			}
		}
		return m
	}
	psConverges(t, dropEmpty, fmt.Sprintf(`resource_grants = { allow = [{ path = %s, verbs = ["GET"] }], deny = [] }`, psPath3()))
	psConverges(t, dropEmpty, `resource_grants = { allow = [], deny = [] }`)
	addEmpty := func(m map[string]any) map[string]any {
		rg, _ := m["resourceGrants"].(map[string]any)
		if rg == nil {
			rg = map[string]any{}
			m["resourceGrants"] = rg
		}
		for _, side := range []string{"allow", "deny"} {
			if _, ok := rg[side]; !ok {
				rg[side] = []any{}
			}
		}
		return m
	}
	psConverges(t, addEmpty, fmt.Sprintf(`resource_grants = { allow = [{ path = %s, verbs = ["GET"] }] }`, psPath3()))
}

// Rule order and path order are semantic and kept exactly as written.
func TestPSGrants_RuleAndPathOrderPreserved(t *testing.T) {
	cfg := `resource_grants = { allow = [
    { path = [{ node_type = "Workspace", node_id = "ws-b" }], verbs = ["GET"] },
    { path = [{ node_type = "Workspace", node_id = "ws-a" }, { node_type = "Environment", node_id = "env-z" }, { node_type = "ResourceGroup", node_id = "rg-m" }], verbs = ["PUT"] },
    { path = [{ node_type = "Workspace", node_id = "ws-c" }], verbs = ["POST"] },
  ] }`
	psConverges(t, nil, cfg,
		resource.TestCheckResourceAttr("duploai_admin_permission_set.t", "resource_grants.allow.0.path.0.node_id", "ws-b"),
		resource.TestCheckResourceAttr("duploai_admin_permission_set.t", "resource_grants.allow.1.path.1.node_id", "env-z"),
		resource.TestCheckResourceAttr("duploai_admin_permission_set.t", "resource_grants.allow.1.path.2.node_id", "rg-m"),
		resource.TestCheckResourceAttr("duploai_admin_permission_set.t", "resource_grants.allow.2.path.0.node_id", "ws-c"),
	)
}

// Pre-migration config (legacy only) while the backend now also returns the
// resourceGrants it derived: no diff, and no resource_grants sent.
func TestPSGrants_LegacyOnlyConfigBackendReturnsBothShapes(t *testing.T) {
	both := func(m map[string]any) map[string]any {
		m["resourceGrants"] = map[string]any{
			"allow": []any{map[string]any{
				"path":  []any{map[string]any{"nodeType": "Workspace", "nodeId": "ws-1", "name": "WS One"}},
				"verbs": []any{"GET"}, "actions": []any{}, "scopes": []any{"read:.*"}, "allowAllChildren": true,
			}},
			"deny": []any{},
		}
		return m
	}
	stub, srv := newPSStub(t, both)
	psUnit(t, srv.URL, resource.TestStep{
		Config: psProvider(srv.URL) + `
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  allowed_workspaces = [{ workspace_id = "ws-1", allowed_scopes = ["read:.*"] }]
}`,
		Check: func(*terraform.State) error {
			for _, b := range stub.bodies("POST") {
				if _, ok := b["resourceGrants"]; ok {
					return fmt.Errorf("legacy-only config sent resourceGrants: %v", b)
				}
			}
			return nil
		},
	})
}
