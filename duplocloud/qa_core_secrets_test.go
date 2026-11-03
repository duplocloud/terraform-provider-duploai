package duplocloud

// core-qa regression tests for the secret attributes of the shipped RDS /
// ElastiCache / AWS secret / Postgres specs (findings F-1, F-2, F-7, F-8).
//
// The mock backend models the platform contract after backend PR #721
// (e1edec893): spec.masterUserPassword / spec.authToken are persisted but
// OMITTED from every response (GET, list, POST/PUT echo). Its PUT handler
// models ResourceServiceBase + the repo hooks for a skill-provisioned
// resource: a spec diff against the stored record flips Complete->Processing
// (OnBeforeUpdateAsync, BEFORE the hook restores an empty secret); the hook
// then restores the stored secret; a reconcile (back to Complete) is only
// dispatched when the stored spec really changed (OnAfterUpdateAsync).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

type qcBackend struct {
	mu        sync.Mutex
	secretKey string         // spec key that is persisted but never returned
	omit      bool           // #721 contract: omit secretKey from responses
	record    map[string]any // stored record, secret included
	puts      []map[string]any
	posts     []map[string]any
	log       []string
}

func (b *qcBackend) respond(w http.ResponseWriter) {
	out := deepCopyMap(b.record)
	if b.omit {
		if sp, ok := out["spec"].(map[string]any); ok {
			delete(sp, b.secretKey)
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
}

// qcCanon drops nulls (the backend's HasSpecChanged treats null as absent).
func qcCanon(v any) any {
	if m, ok := v.(map[string]any); ok {
		o := map[string]any{}
		for k, x := range m {
			if x != nil {
				o[k] = qcCanon(x)
			}
		}
		return o
	}
	return v
}

func (b *qcBackend) handle(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	b.log = append(b.log, r.Method+" "+r.URL.Path)
	switch r.Method {
	case http.MethodPost:
		b.posts = append(b.posts, body)
		body["id"], body["status"] = "obj1", "Complete"
		b.record = body
	case http.MethodPut:
		b.puts = append(b.puts, deepCopyMap(body))
		inSpec, _ := body["spec"].(map[string]any)
		stSpec, _ := b.record["spec"].(map[string]any)
		status := b.record["status"]
		if !reflect.DeepEqual(qcCanon(inSpec), qcCanon(stSpec)) && status == "Complete" {
			status = "Processing" // OnBeforeUpdateAsync
		}
		if inSpec != nil && (inSpec[b.secretKey] == nil || inSpec[b.secretKey] == "") {
			inSpec[b.secretKey] = stSpec[b.secretKey] // OnPreUpdate hook: keep stored
		}
		if status == "Processing" && !reflect.DeepEqual(qcCanon(inSpec), qcCanon(stSpec)) {
			status = "Complete" // real spec change -> reconcile dispatched (agent finishes instantly here)
		}
		body["id"], body["status"] = "obj1", status
		b.record = body
	}
	b.respond(w)
}

type qcHarness struct {
	t  *testing.T
	r  *dynamicResource
	be *qcBackend
	sc tfsdk.State
	ty tftypes.Object
}

func newQCHarness(t *testing.T, specName, secretKey string) *qcHarness {
	t.Helper()
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatal(err)
	}
	var spec *ResourceSpec
	for i := range specs {
		if specs[i].Name == specName {
			spec = &specs[i]
		}
	}
	if spec == nil {
		t.Fatalf("spec %s not found", specName)
	}
	ep, err := spec.BuildEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	be := &qcBackend{secretKey: secretKey, omit: true}
	srv := httptest.NewServer(http.HandlerFunc(be.handle))
	t.Cleanup(srv.Close)
	client, err := duplosdk.NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	h := &qcHarness{t: t, be: be, r: &dynamicResource{baseResource: baseResource{Client: client}, spec: *spec, endpoint: ep}}
	var sr resource.SchemaResponse
	h.r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	if sr.Diagnostics.HasError() {
		t.Fatalf("schema: %v", sr.Diagnostics)
	}
	h.sc = tfsdk.State{Schema: sr.Schema}
	h.ty = sr.Schema.Type().TerraformType(context.Background()).(tftypes.Object)
	return h
}

// val: an object of the resource type, all null except the given attributes.
func (h *qcHarness) val(set map[string]tftypes.Value) tftypes.Value {
	vals := map[string]tftypes.Value{}
	for n, typ := range h.ty.AttributeTypes {
		vals[n] = tftypes.NewValue(typ, nil)
	}
	for k, v := range set {
		vals[k] = v
	}
	return tftypes.NewValue(h.ty, vals)
}

// with returns v with the given attributes replaced.
func (h *qcHarness) with(v tftypes.Value, set map[string]tftypes.Value) tftypes.Value {
	src := map[string]tftypes.Value{}
	_ = v.As(&src)
	m := make(map[string]tftypes.Value, len(src)) // As shares the value's map: copy
	for k, x := range src {
		m[k] = x
	}
	for k, x := range set {
		m[k] = x
	}
	return tftypes.NewValue(h.ty, m)
}

func (h *qcHarness) create(plan, cfg tftypes.Value) tftypes.Value {
	h.t.Helper()
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: h.sc.Schema}}
	h.r.Create(context.Background(), resource.CreateRequest{
		Config: tfsdk.Config{Schema: h.sc.Schema, Raw: cfg},
		Plan:   tfsdk.Plan{Schema: h.sc.Schema, Raw: plan},
	}, resp)
	if resp.Diagnostics.HasError() {
		h.t.Fatalf("Create: %v", resp.Diagnostics)
	}
	return resp.State.Raw
}

func (h *qcHarness) read(prior tftypes.Value) tftypes.Value {
	h.t.Helper()
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: h.sc.Schema, Raw: prior}}
	h.r.Read(context.Background(), resource.ReadRequest{State: tfsdk.State{Schema: h.sc.Schema, Raw: prior}}, resp)
	if resp.Diagnostics.HasError() {
		h.t.Fatalf("Read: %v", resp.Diagnostics)
	}
	return resp.State.Raw
}

// update runs Update with a short deadline so a resource stuck in Processing
// surfaces as an error instead of the 60-minute waiter timeout.
func (h *qcHarness) update(state, plan, cfg tftypes.Value) (*resource.UpdateResponse, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: h.sc.Schema}}
	h.r.Update(ctx, resource.UpdateRequest{
		Config: tfsdk.Config{Schema: h.sc.Schema, Raw: cfg},
		Plan:   tfsdk.Plan{Schema: h.sc.Schema, Raw: plan},
		State:  tfsdk.State{Schema: h.sc.Schema, Raw: state},
	}, resp)
	errs := ""
	for _, d := range resp.Diagnostics.Errors() {
		errs += d.Summary() + ": " + d.Detail() + "; "
	}
	return resp, errs
}

func (h *qcHarness) lastPutHasSecret() (bool, int) {
	h.be.mu.Lock()
	defer h.be.mu.Unlock()
	if len(h.be.puts) == 0 {
		return false, 0
	}
	sp, _ := h.be.puts[len(h.be.puts)-1]["spec"].(map[string]any)
	v, ok := sp[h.be.secretKey]
	return ok && v != nil && v != "", len(h.be.puts)
}

func qcRDSBase(h *qcHarness, extra map[string]tftypes.Value) tftypes.Value {
	set := map[string]tftypes.Value{
		"workspace_id": str("ws1"), "name": str("db1"), "description": str("a"),
		"resource_group_id": str("rg1"), "environment_id": str("env1"),
		"engine": str("postgres"), "db_instance_class": str("db.t3.micro"),
		"scope_ids": tftypes.NewValue(h.ty.AttributeTypes["scope_ids"], []tftypes.Value{str("s1")}),
	}
	for k, v := range extra {
		set[k] = v
	}
	return h.val(set)
}

func qcCacheBase(h *qcHarness, extra map[string]tftypes.Value) tftypes.Value {
	set := map[string]tftypes.Value{
		"workspace_id": str("ws1"), "name": str("c1"), "description": str("a"),
		"resource_group_id": str("rg1"), "environment_id": str("env1"),
		"engine": str("Redis"), "cache_node_type": str("cache.t3.micro"),
	}
	for k, v := range extra {
		set[k] = v
	}
	return h.val(set)
}

// F-1: with the legacy state-stored secret, a backend that never returns the
// secret (#721) must not make Terraform lose it: after create, after refresh
// and after an unrelated update the state must still equal the configured
// value — otherwise every plan shows an update (and create/update fail with
// "inconsistent result after apply", since the plan said the value is kept).
func TestQACore_LegacySecretSurvivesOmittedResponse(t *testing.T) {
	for _, tc := range []struct {
		spec, key, attr string
		base            func(*qcHarness, map[string]tftypes.Value) tftypes.Value
	}{
		{"rds_instance", "masterUserPassword", "master_user_password", qcRDSBase},
		{"rds_cluster", "masterUserPassword", "master_user_password", nil},
		{"elasticache", "authToken", "auth_token", qcCacheBase},
	} {
		for _, omit := range []bool{false, true} {
			name := tc.spec + "/pre-721"
			if omit {
				name = tc.spec + "/post-721"
			}
			t.Run(name, func(t *testing.T) {
				h := newQCHarness(t, tc.spec, tc.key)
				h.be.omit = omit
				base := tc.base
				if base == nil { // rds_cluster: same shape as rds_instance minus class
					base = func(h *qcHarness, x map[string]tftypes.Value) tftypes.Value {
						set := map[string]tftypes.Value{"workspace_id": str("ws1"), "name": str("cl1"), "description": str("a"),
							"resource_group_id": str("rg1"), "environment_id": str("env1"), "engine": str("aurora-postgresql")}
						for k, v := range x {
							set[k] = v
						}
						return h.val(set)
					}
				}
				cfg := base(h, map[string]tftypes.Value{tc.attr: str("pw-1")})
				st := h.create(cfg, cfg)
				if got := attrOf(st, tc.attr); !got.Equal(str("pw-1")) {
					t.Errorf("after Create: %s = %v, want the planned value (else apply fails: inconsistent result)", tc.attr, got)
				}
				st = h.with(st, map[string]tftypes.Value{tc.attr: str("pw-1")}) // continue as if create kept it
				st = h.read(st)
				if got := attrOf(st, tc.attr); !got.Equal(str("pw-1")) {
					t.Errorf("after Read: %s = %v, want pw-1 (null => update planned on every run)", tc.attr, got)
				}
				// The update Terraform then plans (state null -> config pw-1, plus an
				// unrelated change) must end with the planned value in state.
				st = h.with(st, map[string]tftypes.Value{tc.attr: tftypes.NewValue(tftypes.String, nil)})
				plan := h.with(st, map[string]tftypes.Value{tc.attr: str("pw-1"), "description": str("b")})
				resp, errs := h.update(st, plan, plan)
				if errs != "" {
					t.Fatalf("Update: %s", errs)
				}
				if got := attrOf(resp.State.Raw, tc.attr); !got.Equal(str("pw-1")) {
					t.Errorf("after Update: %s = %v, want the planned pw-1 (else: inconsistent result after apply)", tc.attr, got)
				}
			})
		}
	}
}

// F-2: with the write-only secret in use, an update that is not rotating it
// (here: the top-level description, the one settable non-spec field) must not
// send a spec that differs from the stored one. Before #721 the pre-update GET
// carried the secret and writeOnlyInUsePaths kept it; after #721 the GET omits
// it, so the PUT drops the secret, the backend flips Complete->Processing, the
// hook restores the secret, no reconcile is dispatched and the waiter hangs.
func TestQACore_WriteOnlyNonRotatingUpdateAfterSecretOmitted(t *testing.T) {
	for _, tc := range []struct {
		spec, key, wo string
		base          func(*qcHarness, map[string]tftypes.Value) tftypes.Value
	}{
		{"rds_instance", "masterUserPassword", "master_user_password_wo", qcRDSBase},
		{"elasticache", "authToken", "auth_token_wo", qcCacheBase},
	} {
		for _, omit := range []bool{false, true} {
			name := tc.spec + "/pre-721"
			if omit {
				name = tc.spec + "/post-721"
			}
			t.Run(name, func(t *testing.T) {
				h := newQCHarness(t, tc.spec, tc.key)
				h.be.omit = omit
				ver := tc.wo + "_version"
				plan := tc.base(h, map[string]tftypes.Value{ver: num(1)})
				cfg := h.with(plan, map[string]tftypes.Value{tc.wo: str("pw-1")})
				st := h.create(plan, cfg)

				plan2 := h.with(st, map[string]tftypes.Value{"description": str("b")})
				cfg2 := h.with(cfg, map[string]tftypes.Value{"description": str("b")})
				_, errs := h.update(st, plan2, cfg2)
				has, n := h.lastPutHasSecret()
				if n != 1 {
					t.Fatalf("want exactly one PUT, got %d (errs: %s; calls %v)", n, errs, h.be.log)
				}
				if !has {
					t.Errorf("non-rotating update PUT dropped spec.%s (stored secret not re-sent)", tc.key)
				}
				if errs != "" {
					t.Errorf("update must converge, got: %s", errs)
				}
			})
		}
	}
}

// F-8 (a): dropping the write-only argument and its version from config,
// with nothing else changed, must not issue any API call.
// F-8 (b): dropping it together with another change sends a PUT without the
// secret (nothing to send: config no longer has it). That is the documented
// "backend keeps the stored secret" path; the stuck-Processing outcome on a
// description-only change is a backend defect (OnBeforeUpdateAsync compares
// before the hook restores the secret), recorded as a characterization here.
func TestQACore_WriteOnlyRemovedFromConfig(t *testing.T) {
	h := newQCHarness(t, "rds_instance", "masterUserPassword")
	h.be.omit = false // pre-#721 platform: GET still returns the secret
	plan := qcRDSBase(h, map[string]tftypes.Value{"master_user_password_wo_version": num(1)})
	cfg := h.with(plan, map[string]tftypes.Value{"master_user_password_wo": str("pw-1")})
	st := h.create(plan, cfg)

	// (a) only the version (and value) removed
	plan2 := h.with(st, map[string]tftypes.Value{"master_user_password_wo_version": tftypes.NewValue(tftypes.Number, nil)})
	cfg2 := h.with(cfg, map[string]tftypes.Value{"master_user_password_wo": tftypes.NewValue(tftypes.String, nil),
		"master_user_password_wo_version": tftypes.NewValue(tftypes.Number, nil)})
	if _, errs := h.update(st, plan2, cfg2); errs != "" {
		t.Fatalf("removal-only update: %s", errs)
	}
	if _, n := h.lastPutHasSecret(); n != 0 {
		t.Errorf("removing only the write-only argument must not call the API, got %d PUT(s)", n)
	}

	// (b) removal plus a spec change (engine_version) on a pre-#721 platform:
	// the GET carries the secret but the keep path is gone, so it is deleted.
	plan3 := h.with(plan2, map[string]tftypes.Value{"engine_version": str("16.4")})
	cfg3 := h.with(cfg2, map[string]tftypes.Value{"engine_version": str("16.4")})
	_, errs := h.update(st, plan3, cfg3)
	has, n := h.lastPutHasSecret()
	if n != 1 || has {
		t.Errorf("removal + change: want one PUT without the secret, got n=%d hasSecret=%v (errs %s)", n, has, errs)
	}
	if errs != "" {
		t.Errorf("a real spec change must still converge (reconcile dispatched), got %s", errs)
	}
}

// F-7: the legacy secret attributes have no "send only if changed" guard, so
// an update of an unrelated field re-sends the secret in the update request.
// Characterization (pre-existing on develop; not introduced by this branch).
func TestQACore_LegacySecretResentOnUnrelatedUpdate(t *testing.T) {
	for _, tc := range []struct {
		spec, path string
		state      map[string]tftypes.Value
		change     map[string]tftypes.Value
	}{
		{"aws_secret", "spec.updateRequest.secretString",
			map[string]tftypes.Value{"workspace_id": str("ws1"), "id": str("ws1/obj1"), "name": str("s1"), "resource_group_id": str("rg1"),
				"environment_id": str("env1"), "description": str("a"), "secret_string": str("v1")},
			map[string]tftypes.Value{"description": str("b")}},
		{"azure_postgres_flexible_server", "spec.updateRequest.administratorLoginPassword",
			map[string]tftypes.Value{"workspace_id": str("ws1"), "id": str("ws1/obj1"), "name": str("pg1"), "resource_group_id": str("rg1"),
				"environment_id": str("env1"), "administrator_password": str("v1")},
			map[string]tftypes.Value{"tags": tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{"team": str("qa")})}},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			h := newQCHarness(t, tc.spec, "unused")
			h.be.record = map[string]any{"id": "obj1", "status": "Complete", "name": "x", "spec": map[string]any{}}
			st := h.val(tc.state)
			plan := h.with(st, tc.change)
			_, _ = h.update(st, plan, plan)
			h.be.mu.Lock()
			defer h.be.mu.Unlock()
			if len(h.be.puts) != 1 {
				t.Fatalf("want one PUT, got %d", len(h.be.puts))
			}
			v, ok := lookupPath(h.be.puts[0], strings.Split(tc.path, "."))
			t.Logf("%s: unrelated update sends %s: %v", tc.spec, tc.path, ok && v == "v1")
			if !ok || v != "v1" {
				t.Errorf("characterization changed: %s no longer re-sent on an unrelated update", tc.path)
			}
		})
	}
}

// F-9: aws_secret with the write-only secret in use. A description-only update
// sends no spec.updateRequest, so the backend sees no spec change, skips the
// passthrough's UpdateDirectAsync (the only thing that sets Complete) and
// persists the body's status — absent, so the default New — with the result
// wiped (seen live). No worker reconciles a passthrough, so the update waiter
// never sees Complete. The mock models ResourceServiceBase.UpdateAsync for a
// passthrough: direct update (status Complete) only when the spec changed,
// otherwise the record is replaced by the body as sent.
func TestQACore_AwsSecretWriteOnlyDescriptionOnlyUpdate(t *testing.T) {
	h := newQCHarness(t, "aws_secret", "unused")
	h.be.record = map[string]any{"id": "obj1", "status": "Complete", "name": "s1", "description": "a",
		"spec": map[string]any{"resourceGroupId": "rg1", "environmentId": "env1", "secretValueType": "PlainText"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.be.mu.Lock()
		defer h.be.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		if r.Method == http.MethodPut {
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			h.be.puts = append(h.be.puts, deepCopyMap(body))
			changed := !reflect.DeepEqual(qcCanon(body["spec"]), qcCanon(h.be.record["spec"]))
			if spec, ok := body["spec"].(map[string]any); ok {
				delete(spec, "updateRequest") // BsonIgnore
			}
			if changed {
				body["status"] = "Complete" // UpdateDirectAsync
			} else if body["status"] == nil {
				body["status"] = "New" // enum default persisted
			}
			body["id"] = "obj1"
			h.be.record = body
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": h.be.record})
	}))
	t.Cleanup(srv.Close)
	client, _ := duplosdk.NewClient(srv.URL, "tok", false, 5*time.Second)
	h.r.Client = client

	common := map[string]tftypes.Value{"workspace_id": str("ws1"), "id": str("ws1/obj1"), "name": str("s1"),
		"resource_group_id": str("rg1"), "environment_id": str("env1"), "secret_value_type": str("PlainText"),
		"status": str("Complete"), "aws_secret_id": str("obj1"), "secret_string_wo_version": num(1)}
	st := h.val(common)
	st = h.with(st, map[string]tftypes.Value{"description": str("a")})
	plan := h.with(st, map[string]tftypes.Value{"description": str("b")})
	cfg := h.with(plan, map[string]tftypes.Value{"secret_string_wo": str("v1")})
	_, errs := h.update(st, plan, cfg)
	if len(h.be.puts) != 1 {
		t.Fatalf("want one PUT, got %d (%s)", len(h.be.puts), errs)
	}
	if errs != "" {
		t.Errorf("description-only update must converge; got %s (record status now %v)", errs, h.be.record["status"])
	}
}
