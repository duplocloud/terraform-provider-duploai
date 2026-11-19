package duplocloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// ── adv-qa shared protocol-server harness ────────────────────────────────────

type qaAdvHarness struct {
	t       *testing.T
	ctx     context.Context
	server  tfprotov6.ProviderServer
	schemas *tfprotov6.GetProviderSchemaResponse
	ids     *tfprotov6.GetResourceIdentitySchemasResponse

	mu    sync.Mutex
	paths []string // METHOD path, every request the fake backend saw
}

func (h *qaAdvHarness) record(r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.paths = append(h.paths, r.Method+" "+r.URL.Path)
}

func (h *qaAdvHarness) seen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.paths...)
}

// newQAAdvHarness starts a configured provider protocol server against a fake
// backend. handler may be nil until the test sets one via the returned setter.
func newQAAdvHarness(t *testing.T, handler http.HandlerFunc) *qaAdvHarness {
	t.Helper()
	h := &qaAdvHarness{t: t, ctx: context.Background()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.record(r)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	server, err := providerserver.NewProtocol6WithError(New())()
	if err != nil {
		t.Fatal(err)
	}
	h.server = server
	if h.schemas, err = server.GetProviderSchema(h.ctx, &tfprotov6.GetProviderSchemaRequest{}); err != nil {
		t.Fatal(err)
	}
	if h.ids, err = server.GetResourceIdentitySchemas(h.ctx, &tfprotov6.GetResourceIdentitySchemasRequest{}); err != nil {
		t.Fatal(err)
	}
	provType := h.schemas.Provider.ValueType().(tftypes.Object)
	pvals := map[string]tftypes.Value{}
	for n, typ := range provType.AttributeTypes {
		pvals[n] = tftypes.NewValue(typ, nil)
	}
	pvals["duplo_host"] = tftypes.NewValue(tftypes.String, srv.URL)
	pvals["duplo_token"] = tftypes.NewValue(tftypes.String, "tok")
	pcfg, err := tfprotov6.NewDynamicValue(provType, tftypes.NewValue(provType, pvals))
	if err != nil {
		t.Fatal(err)
	}
	cresp, err := server.ConfigureProvider(h.ctx, &tfprotov6.ConfigureProviderRequest{Config: &pcfg})
	if err != nil {
		t.Fatal(err)
	}
	if e := qaAdvErr(cresp.Diagnostics); e != "" {
		t.Fatalf("ConfigureProvider: %s", e)
	}
	return h
}

func qaAdvErr(ds []*tfprotov6.Diagnostic) string {
	var out []string
	for _, d := range ds {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			out = append(out, d.Summary+": "+d.Detail)
		}
	}
	return strings.Join(out, " | ")
}

func qaAdvWarn(ds []*tfprotov6.Diagnostic) []string {
	var out []string
	for _, d := range ds {
		if d.Severity == tfprotov6.DiagnosticSeverityWarning {
			out = append(out, d.Summary)
		}
	}
	return out
}

func (h *qaAdvHarness) resType(name string) tftypes.Object {
	return h.schemas.ResourceSchemas[name].ValueType().(tftypes.Object)
}

// obj builds a value of the resource type: every attribute null except set.
func (h *qaAdvHarness) obj(name string, set map[string]tftypes.Value) tftypes.Value {
	typ := h.resType(name)
	vals := map[string]tftypes.Value{}
	for n, at := range typ.AttributeTypes {
		if v, ok := set[n]; ok {
			vals[n] = v
		} else {
			vals[n] = tftypes.NewValue(at, nil)
		}
	}
	return tftypes.NewValue(typ, vals)
}

func (h *qaAdvHarness) dv(name string, v tftypes.Value) *tfprotov6.DynamicValue {
	d, err := tfprotov6.NewDynamicValue(h.resType(name), v)
	if err != nil {
		h.t.Fatal(err)
	}
	return &d
}

func (h *qaAdvHarness) identity(name string, vals map[string]tftypes.Value) *tfprotov6.ResourceIdentityData {
	idType := h.ids.IdentitySchemas[name].ValueType().(tftypes.Object)
	full := map[string]tftypes.Value{}
	for n := range idType.AttributeTypes {
		if v, ok := vals[n]; ok {
			full[n] = v
		} else {
			full[n] = tftypes.NewValue(tftypes.String, nil)
		}
	}
	d, err := tfprotov6.NewDynamicValue(idType, tftypes.NewValue(idType, full))
	if err != nil {
		h.t.Fatal(err)
	}
	return &tfprotov6.ResourceIdentityData{IdentityData: &d}
}

func (h *qaAdvHarness) identityMap(name string, d *tfprotov6.ResourceIdentityData) map[string]string {
	if d == nil || d.IdentityData == nil {
		return nil
	}
	idType := h.ids.IdentitySchemas[name].ValueType().(tftypes.Object)
	v, err := d.IdentityData.Unmarshal(idType)
	if err != nil {
		h.t.Fatal(err)
	}
	var m map[string]tftypes.Value
	if err := v.As(&m); err != nil {
		h.t.Fatal(err)
	}
	out := map[string]string{}
	for k, tv := range m {
		var s string
		if tv.IsKnown() && !tv.IsNull() {
			_ = tv.As(&s)
		}
		out[k] = s
	}
	return out
}

func qaAdvStr(t *testing.T, v tftypes.Value, attr string) string {
	t.Helper()
	var m map[string]tftypes.Value
	if err := v.As(&m); err != nil {
		t.Fatal(err)
	}
	var s string
	if m[attr].IsKnown() && !m[attr].IsNull() {
		_ = m[attr].As(&s)
	}
	return s
}

const qaAdvRG = "duploai_resource_group"

func qaAdvRGHandler(w http.ResponseWriter, r *http.Request) {
	// any GET of a single RG answers with object obj1 in workspace ws1
	_, _ = w.Write([]byte(`{"data":{"id":"obj1","name":"rg1","status":"Complete"}}`))
}

// ── F-5: import ID / identity with extra or slash-bearing segments ───────────

// An import ID with more segments than the resource has must not be accepted
// silently: today "ws1/obj1/junk" imports obj1, and the junk stays in the
// state id forever, while the identity Terraform stores is {ws1, obj1}.
// Expect either an "Invalid import ID" error or a canonical id.
func TestQAAdv_ImportID_ExtraTrailingSegments(t *testing.T) {
	h := newQAAdvHarness(t, qaAdvRGHandler)
	resp, err := h.server.ImportResourceState(h.ctx, &tfprotov6.ImportResourceStateRequest{TypeName: qaAdvRG, ID: "ws1/obj1/junk"})
	if err != nil {
		t.Fatal(err)
	}
	if qaAdvErr(resp.Diagnostics) != "" {
		return // rejected up front: fine
	}
	st, err := resp.ImportedResources[0].State.Unmarshal(h.resType(qaAdvRG))
	if err != nil {
		t.Fatal(err)
	}
	// Follow with the Read Terraform performs after import.
	rr, err := h.server.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{
		TypeName: qaAdvRG, CurrentState: resp.ImportedResources[0].State, Private: resp.ImportedResources[0].Private,
		CurrentIdentity: resp.ImportedResources[0].Identity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if e := qaAdvErr(rr.Diagnostics); e != "" {
		t.Fatalf("read after import: %s", e)
	}
	ns, err := rr.NewState.Unmarshal(h.resType(qaAdvRG))
	if err != nil {
		t.Fatal(err)
	}
	ident := h.identityMap(qaAdvRG, rr.NewIdentity)
	gotID := qaAdvStr(t, ns, "id")
	if gotID != ident["workspace_id"]+"/"+ident["id"] {
		t.Errorf("import ID %q accepted: state id after read = %q (import state id %q) but identity = %v; "+
			"want the extra segment rejected or the id canonicalised", "ws1/obj1/junk", gotID, qaAdvStr(t, st, "id"), ident)
	}
}

// Import by identity joins the identity values with "/" without checking them,
// so a value carrying "/" shifts every later segment: {workspace_id: ws1,
// id: "obj1/extra"} imports obj1, and {workspace_id: "ws1/x", id: "obj1"}
// reads object "x" in workspace "ws1" — an object the user never named. The
// stored identity then differs from the one in the import block (the framework
// skips the identity-change check on the read that follows an import).
func TestQAAdv_ImportIdentity_SlashInValue(t *testing.T) {
	cases := map[string]map[string]tftypes.Value{
		"id has slash": {
			"workspace_id": tftypes.NewValue(tftypes.String, "ws1"),
			"id":           tftypes.NewValue(tftypes.String, "obj1/extra"),
		},
		"workspace_id has slash": {
			"workspace_id": tftypes.NewValue(tftypes.String, "ws1/x"),
			"id":           tftypes.NewValue(tftypes.String, "obj1"),
		},
	}
	for label, vals := range cases {
		t.Run(label, func(t *testing.T) {
			h := newQAAdvHarness(t, qaAdvRGHandler)
			resp, err := h.server.ImportResourceState(h.ctx, &tfprotov6.ImportResourceStateRequest{
				TypeName: qaAdvRG, Identity: h.identity(qaAdvRG, vals),
			})
			if err != nil {
				t.Fatal(err)
			}
			if qaAdvErr(resp.Diagnostics) != "" {
				return // rejected: fine
			}
			rr, err := h.server.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{
				TypeName: qaAdvRG, CurrentState: resp.ImportedResources[0].State, Private: resp.ImportedResources[0].Private,
				CurrentIdentity: resp.ImportedResources[0].Identity,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := h.identityMap(qaAdvRG, rr.NewIdentity)
			var want = map[string]string{}
			for k, v := range vals {
				var s string
				_ = v.As(&s)
				want[k] = s
			}
			if got["id"] != want["id"] || got["workspace_id"] != want["workspace_id"] {
				t.Errorf("import identity %v accepted, but the object read was %v (requests: %v); "+
					"want identity values containing '/' rejected", want, got, h.seen())
			}
		})
	}
}

// A missing identity attribute (null) must be rejected, not imported as an
// empty segment.
func TestQAAdv_ImportIdentity_MissingAttribute(t *testing.T) {
	h := newQAAdvHarness(t, qaAdvRGHandler)
	resp, err := h.server.ImportResourceState(h.ctx, &tfprotov6.ImportResourceStateRequest{
		TypeName: qaAdvRG, Identity: h.identity(qaAdvRG, map[string]tftypes.Value{
			"id": tftypes.NewValue(tftypes.String, "obj1"),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if qaAdvErr(resp.Diagnostics) == "" {
		t.Errorf("identity without workspace_id was accepted")
	}
	if len(h.seen()) != 0 {
		t.Errorf("no backend call expected, got %v", h.seen())
	}
}

// Read with a malformed or foreign prior identity: never a panic, never a
// silent identity swap — the framework must report it.
func TestQAAdv_ReadResource_ForeignPriorIdentity(t *testing.T) {
	h := newQAAdvHarness(t, qaAdvRGHandler)
	state := h.dv(qaAdvRG, h.obj(qaAdvRG, map[string]tftypes.Value{
		"id":           tftypes.NewValue(tftypes.String, "ws1/obj1"),
		"workspace_id": tftypes.NewValue(tftypes.String, "ws1"),
	}))
	foreign := h.identity(qaAdvRG, map[string]tftypes.Value{
		"workspace_id": tftypes.NewValue(tftypes.String, "ws9"),
		"id":           tftypes.NewValue(tftypes.String, "other"),
	})
	rr, err := h.server.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{TypeName: qaAdvRG, CurrentState: state, CurrentIdentity: foreign})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(qaAdvErr(rr.Diagnostics), "Unexpected Identity Change") {
		t.Errorf("foreign prior identity: want 'Unexpected Identity Change', got %q identity=%v", qaAdvErr(rr.Diagnostics), h.identityMap(qaAdvRG, rr.NewIdentity))
	}

	// Partially null prior identity (e.g. hand-edited state): reported, not swapped silently.
	partial := h.identity(qaAdvRG, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "obj1")})
	rr, err = h.server.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{TypeName: qaAdvRG, CurrentState: state, CurrentIdentity: partial})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("partial prior identity -> err=%q identity=%v", qaAdvErr(rr.Diagnostics), h.identityMap(qaAdvRG, rr.NewIdentity))

	// Malformed identity JSON must be a diagnostic, not a panic.
	bad := &tfprotov6.ResourceIdentityData{IdentityData: &tfprotov6.DynamicValue{JSON: []byte(`{"workspace_id":`)}}
	rr, err = h.server.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{TypeName: qaAdvRG, CurrentState: state, CurrentIdentity: bad})
	if err != nil {
		t.Fatal(err)
	}
	if qaAdvErr(rr.Diagnostics) == "" {
		t.Errorf("malformed prior identity JSON accepted")
	}
}

// UpgradeResourceState / UpgradeResourceIdentity at version 0 (every spec):
// malformed JSON is a diagnostic; foreign identity JSON with an unknown
// attribute is reported, not silently dropped.
func TestQAAdv_UpgradeState_MalformedAndForeign(t *testing.T) {
	h := newQAAdvHarness(t, qaAdvRGHandler)
	ur, err := h.server.UpgradeResourceState(h.ctx, &tfprotov6.UpgradeResourceStateRequest{
		TypeName: qaAdvRG, Version: 0, RawState: &tfprotov6.RawState{JSON: []byte(`{"id":`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if qaAdvErr(ur.Diagnostics) == "" {
		t.Errorf("malformed raw state accepted")
	}
	// A state from a FUTURE version (downgraded provider) must be refused.
	ur, err = h.server.UpgradeResourceState(h.ctx, &tfprotov6.UpgradeResourceStateRequest{
		TypeName: qaAdvRG, Version: 3, RawState: &tfprotov6.RawState{JSON: []byte(`{"id":"ws1/obj1","workspace_id":"ws1"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("future-version state -> err=%q", qaAdvErr(ur.Diagnostics))

	ir, err := h.server.UpgradeResourceIdentity(h.ctx, &tfprotov6.UpgradeResourceIdentityRequest{
		TypeName: qaAdvRG, Version: 0,
		RawIdentity: &tfprotov6.RawState{JSON: []byte(`{"workspace_id":"ws1","id":"obj1","tenant_id":"t"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if qaAdvErr(ir.Diagnostics) == "" {
		t.Errorf("identity JSON with an attribute outside the identity schema accepted silently: %v", h.identityMap(qaAdvRG, ir.UpgradedIdentity))
	}
}

// Ignored-change warning under unknown values: an unknown createOnly value
// (from another resource) cannot be judged at plan time and must not error;
// an unknown UNRELATED value must not suppress the warning on a known
// createOnly change.
func TestQAAdv_WarnIgnoredChanges_Unknowns(t *testing.T) {
	attrs := []AttributeSpec{
		{Name: "workspace_id", Type: "string", Required: true, ForceNew: true},
		{Name: "name", Type: "string", Required: true, ForceNew: true, APIPath: "name"},
		{Name: "description", Type: "string", Optional: true, APIPath: "description"},
		{Name: "tags", Type: "string", Optional: true, CreateOnly: true, RequestPath: "spec.tags"},
	}
	ctx := context.Background()
	r := &dynamicResource{spec: ResourceSpec{Name: "thing", IDPath: "id", Attributes: attrs},
		endpoint: duplosdk.Endpoint{UriBase: "/x/{workspace_id}", Update: duplosdk.Operation{Verb: http.MethodPut}}}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	mk := func(v map[string]any) tftypes.Value {
		vals := map[string]tftypes.Value{}
		for n, at := range typ.AttributeTypes {
			vals[n] = tftypes.NewValue(at, nil)
		}
		for k, s := range v {
			vals[k] = tftypes.NewValue(tftypes.String, s)
		}
		return tftypes.NewValue(typ, vals)
	}
	base := map[string]any{"id": "ws/1", "workspace_id": "ws", "name": "n", "description": "d", "tags": "a"}
	with := func(kv ...any) map[string]any {
		m := map[string]any{}
		for a, b := range base {
			m[a] = b
		}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	run := func(plan map[string]any) []string {
		req := resource.ModifyPlanRequest{
			State:  tfsdk.State{Schema: sr.Schema, Raw: mk(base)},
			Plan:   tfsdk.Plan{Schema: sr.Schema, Raw: mk(plan)},
			Config: tfsdk.Config{Schema: sr.Schema, Raw: mk(plan)},
		}
		resp := &resource.ModifyPlanResponse{Plan: req.Plan}
		r.ModifyPlan(ctx, req, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("unknowns must never error: %v", resp.Diagnostics)
		}
		var got []string
		for _, d := range resp.Diagnostics.Warnings() {
			got = append(got, d.Summary())
		}
		return got
	}
	if got := run(with("tags", tftypes.UnknownValue)); len(got) != 0 {
		t.Errorf("unknown createOnly value: no warning expected at plan time, got %v", got)
	}
	if got := run(with("tags", "b", "description", tftypes.UnknownValue)); len(got) != 1 {
		t.Errorf("unknown unrelated value must not hide the createOnly warning, got %v", got)
	}
}
