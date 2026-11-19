package duplocloud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// DUPLOAI-5637 sanity rows: drive the real rds_cluster / rds_instance specs through
// providerserver against an httptest backend that stores what it is sent, on a
// non-default context path, and fills spec.storageKmsKeyId with the platform's key
// when the request omits it.

const (
	kmsPlatformKey = "arn:aws:kms:eu-west-3:111122223333:key/platform-default-0001"
	kmsARN1        = "arn:aws:kms:eu-west-3:111122223333:key/11111111-aaaa-bbbb-cccc-000000000001"
	kmsARN2        = "arn:aws:kms:eu-west-3:111122223333:alias/other-key"
	kmsCtxPath     = "/duplo-ctx"
)

type kmsBackend struct {
	mu       sync.Mutex
	objs     map[string]map[string]any
	requests []string
	posted   []map[string]any
	fail     int // status to return on POST (0 = ok)
}

func (b *kmsBackend) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.requests = append(b.requests, r.Method+" "+r.URL.Path)
		if !strings.HasPrefix(r.URL.Path, kmsCtxPath+"/v1/") {
			http.Error(w, "wrong context path "+r.URL.Path, http.StatusNotFound)
			return
		}
		var body, sent map[string]any
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
			_ = json.Unmarshal(raw, &sent) // untouched copy of what was sent
		}
		segs := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
		last := segs[len(segs)-1]
		write := func(o map[string]any) {
			out, _ := json.Marshal(map[string]any{"data": o})
			_, _ = w.Write(out)
		}
		switch r.Method {
		case http.MethodPost:
			b.posted = append(b.posted, sent)
			if b.fail != 0 {
				http.Error(w, `{"message":"upstream boom"}`, b.fail)
				return
			}
			spec, _ := body["spec"].(map[string]any)
			if spec == nil {
				spec = map[string]any{}
			}
			if _, ok := spec["storageKmsKeyId"]; !ok {
				spec["storageKmsKeyId"] = kmsPlatformKey
			}
			o := map[string]any{"id": "rds1", "name": body["name"], "status": "Complete", "spec": spec}
			b.objs["rds1"] = o
			write(o)
		case http.MethodGet:
			if o, ok := b.objs[last]; ok {
				write(o)
				return
			}
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		default:
			http.Error(w, "unexpected", http.StatusMethodNotAllowed)
		}
	}
}

type kmsHarness struct {
	t        *testing.T
	ctx      context.Context
	server   tfprotov6.ProviderServer
	backend  *kmsBackend
	typeName string
	rs       *tfprotov6.Schema
	resType  tftypes.Object
}

func newKmsHarness(t *testing.T, specName string) *kmsHarness {
	t.Helper()
	b := &kmsBackend{objs: map[string]map[string]any{}}
	srv := httptest.NewServer(b.handler(t))
	t.Cleanup(srv.Close)
	server, err := providerserver.NewProtocol6WithError(New())()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil || len(schemas.Diagnostics) > 0 {
		t.Fatalf("GetProviderSchema: %v %v", err, schemas.Diagnostics)
	}
	provType := schemas.Provider.ValueType().(tftypes.Object)
	pcfg := protoDynamic(t, provType, objectWith(provType, map[string]tftypes.Value{
		"duplo_host":  tftypes.NewValue(tftypes.String, srv.URL+kmsCtxPath),
		"duplo_token": tftypes.NewValue(tftypes.String, "tok"),
	}))
	conf, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &pcfg})
	if err != nil || len(conf.Diagnostics) > 0 {
		t.Fatalf("ConfigureProvider: %v %v", err, conf.Diagnostics)
	}
	typeName := "duploai_" + specName
	rs := schemas.ResourceSchemas[typeName]
	if rs == nil {
		t.Fatalf("no schema for %s", typeName)
	}
	return &kmsHarness{t: t, ctx: ctx, server: server, backend: b, typeName: typeName, rs: rs,
		resType: rs.ValueType().(tftypes.Object)}
}

// config builds the minimal required config plus kms_key_id (nil Value = omitted).
func (h *kmsHarness) config(kms *tftypes.Value) tftypes.Value {
	set := map[string]tftypes.Value{
		"workspace_id":      tftypes.NewValue(tftypes.String, "ws"),
		"name":              tftypes.NewValue(tftypes.String, "db1"),
		"scope_ids":         tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, "s")}),
		"resource_group_id": tftypes.NewValue(tftypes.String, "rg"),
		"environment_id":    tftypes.NewValue(tftypes.String, "env"),
		"engine":            tftypes.NewValue(tftypes.String, "postgres"),
	}
	if _, ok := h.resType.AttributeTypes["db_instance_class"]; ok {
		set["db_instance_class"] = tftypes.NewValue(tftypes.String, "db.t3.micro")
	}
	for _, a := range h.rs.Block.Attributes {
		if a.Required {
			if _, ok := set[a.Name]; !ok {
				h.t.Fatalf("%s: required attribute %s not covered by harness config", h.typeName, a.Name)
			}
		}
	}
	if kms != nil {
		set["kms_key_id"] = *kms
	}
	return objectWith(h.resType, set)
}

func (h *kmsHarness) validate(cfg tftypes.Value) []*tfprotov6.Diagnostic {
	dv := protoDynamic(h.t, h.resType, cfg)
	resp, err := h.server.ValidateResourceConfig(h.ctx, &tfprotov6.ValidateResourceConfigRequest{TypeName: h.typeName, Config: &dv})
	if err != nil {
		h.t.Fatal(err)
	}
	return resp.Diagnostics
}

func (h *kmsHarness) plan(prior, cfg tftypes.Value) *tfprotov6.PlanResourceChangeResponse {
	h.t.Helper()
	priorDV := protoDynamic(h.t, h.resType, prior)
	cfgDV := protoDynamic(h.t, h.resType, cfg)
	proposed := cfg
	if !prior.IsNull() {
		proposed = proposedNewState(h.rs, h.resType, prior, cfg)
	}
	propDV := protoDynamic(h.t, h.resType, proposed)
	resp, err := h.server.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName: h.typeName, PriorState: &priorDV, ProposedNewState: &propDV, Config: &cfgDV,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}

func (h *kmsHarness) create(cfg tftypes.Value) (tftypes.Value, []*tfprotov6.Diagnostic) {
	h.t.Helper()
	null := tftypes.NewValue(h.resType, nil)
	p := h.plan(null, cfg)
	if e := qaAdvErr(p.Diagnostics); e != "" {
		h.t.Fatalf("create plan: %s", e)
	}
	priorDV := protoDynamic(h.t, h.resType, null)
	cfgDV := protoDynamic(h.t, h.resType, cfg)
	a, err := h.server.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName: h.typeName, PriorState: &priorDV, PlannedState: p.PlannedState, Config: &cfgDV,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	if a.NewState == nil {
		return null, a.Diagnostics
	}
	st, err := a.NewState.Unmarshal(h.resType)
	if err != nil {
		h.t.Fatal(err)
	}
	return st, a.Diagnostics
}

func attrVal(t *testing.T, v tftypes.Value, name string) tftypes.Value {
	t.Helper()
	var m map[string]tftypes.Value
	if err := v.As(&m); err != nil {
		t.Fatal(err)
	}
	return m[name]
}

func strPtrVal(s string) *tftypes.Value { v := tftypes.NewValue(tftypes.String, s); return &v }

// "" and whitespace fail at validate (plan) time with the patternDescription, and the
// backend never sees a request.
func TestKmsHarness_EmptyFailsBeforeAnyAPICall(t *testing.T) {
	for _, n := range kmsSpecs {
		for _, v := range []string{"", " ", "\t\n"} {
			h := newKmsHarness(t, n)
			diags := h.validate(h.config(strPtrVal(v)))
			e := qaAdvErr(diags)
			if !strings.Contains(e, kmsPatternDescription) {
				t.Errorf("%s kms_key_id=%q: diagnostics %q, want %q", n, v, e, kmsPatternDescription)
			}
			for _, d := range diags {
				if d.Severity == tfprotov6.DiagnosticSeverityError && d.Attribute != nil &&
					!d.Attribute.Equal(tftypes.NewAttributePath().WithAttributeName("kms_key_id")) {
					t.Errorf("%s: error on %v, want kms_key_id", n, d.Attribute)
				}
			}
			if len(h.backend.requests) != 0 {
				t.Errorf("%s: backend saw %v, want no request", n, h.backend.requests)
			}
		}
	}
}

// An unknown value (e.g. another resource's arn) passes validation.
func TestKmsHarness_UnknownPassesValidation(t *testing.T) {
	for _, n := range kmsSpecs {
		h := newKmsHarness(t, n)
		u := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
		if e := qaAdvErr(h.validate(h.config(&u))); e != "" {
			t.Errorf("%s: unknown kms_key_id rejected: %s", n, e)
		}
		nullV := tftypes.NewValue(tftypes.String, nil)
		if e := qaAdvErr(h.validate(h.config(&nullV))); e != "" {
			t.Errorf("%s: null kms_key_id rejected: %s", n, e)
		}
	}
}

// Omitted: create sends no storageKmsKeyId, state takes the platform key, the next
// plan is empty and requires no replacement.
func TestKmsHarness_OmittedTakesPlatformKeyNoDiff(t *testing.T) {
	for _, n := range kmsSpecs {
		t.Run(n, func(t *testing.T) {
			h := newKmsHarness(t, n)
			cfg := h.config(nil)
			st, diags := h.create(cfg)
			if e := qaAdvErr(diags); e != "" {
				t.Fatalf("apply: %s", e)
			}
			spec, _ := h.backend.posted[0]["spec"].(map[string]any)
			if _, ok := spec["storageKmsKeyId"]; ok {
				t.Errorf("POST carries storageKmsKeyId=%v for an omitted kms_key_id", spec["storageKmsKeyId"])
			}
			if got := attrVal(t, st, "kms_key_id"); !got.Equal(tftypes.NewValue(tftypes.String, kmsPlatformKey)) {
				t.Errorf("state kms_key_id = %v, want platform key", got)
			}
			p := h.plan(st, cfg)
			if e := qaAdvErr(p.Diagnostics); e != "" {
				t.Fatalf("second plan: %s", e)
			}
			if len(p.RequiresReplace) > 0 {
				t.Errorf("second plan requires replace: %v", p.RequiresReplace)
			}
			planned, _ := p.PlannedState.Unmarshal(h.resType)
			if !planned.Equal(st) {
				diffs, _ := st.Diff(planned)
				t.Errorf("second plan not empty: %v", diffs)
			}
		})
	}
}

// Explicit non-default-region ARN reaches spec.storageKmsKeyId exactly and is read back;
// changing it to a different ARN plans RequiresReplace on kms_key_id (v0.7.0 behaviour);
// keeping it plans no diff.
func TestKmsHarness_ExplicitARNSentAndChangeReplaces(t *testing.T) {
	for _, n := range kmsSpecs {
		t.Run(n, func(t *testing.T) {
			h := newKmsHarness(t, n)
			cfg := h.config(strPtrVal(kmsARN1))
			st, diags := h.create(cfg)
			if e := qaAdvErr(diags); e != "" {
				t.Fatalf("apply: %s", e)
			}
			if !strings.HasPrefix(h.backend.requests[0], "POST "+kmsCtxPath+"/v1/aiservicedesk/user/data/workspaces/ws/environment/") {
				t.Errorf("first request %q, want POST on the context path", h.backend.requests[0])
			}
			spec, _ := h.backend.posted[0]["spec"].(map[string]any)
			if spec["storageKmsKeyId"] != kmsARN1 {
				t.Errorf("POST spec.storageKmsKeyId = %v, want %s", spec["storageKmsKeyId"], kmsARN1)
			}
			if got := attrVal(t, st, "kms_key_id"); !got.Equal(tftypes.NewValue(tftypes.String, kmsARN1)) {
				t.Errorf("state kms_key_id = %v", got)
			}
			same := h.plan(st, cfg)
			if len(same.RequiresReplace) > 0 || qaAdvErr(same.Diagnostics) != "" {
				t.Errorf("same ARN: replace=%v diags=%s", same.RequiresReplace, qaAdvErr(same.Diagnostics))
			}
			chg := h.plan(st, h.config(strPtrVal(kmsARN2)))
			if e := qaAdvErr(chg.Diagnostics); e != "" {
				t.Fatalf("change plan: %s", e)
			}
			want := tftypes.NewAttributePath().WithAttributeName("kms_key_id")
			found := false
			for _, p := range chg.RequiresReplace {
				if p.Equal(want) {
					found = true
				}
			}
			if !found {
				t.Errorf("ARN1->ARN2 RequiresReplace = %v, want kms_key_id", chg.RequiresReplace)
			}
		})
	}
}

// Import with kms_key_id omitted from config: imported+read state plans with no diff.
func TestKmsHarness_ImportOmittedNoDiff(t *testing.T) {
	for _, n := range kmsSpecs {
		t.Run(n, func(t *testing.T) {
			h := newKmsHarness(t, n)
			h.backend.objs["rds1"] = map[string]any{"id": "rds1", "name": "db1", "status": "Complete", "spec": map[string]any{
				"scopeIds": []any{"s"}, "resourceGroupId": "rg", "environmentId": "env", "engine": "postgres",
				"dbInstanceClass": "db.t3.micro", "storageKmsKeyId": kmsPlatformKey,
			}}
			imp, err := h.server.ImportResourceState(h.ctx, &tfprotov6.ImportResourceStateRequest{TypeName: h.typeName, ID: "ws/rds1"})
			if err != nil || qaAdvErr(imp.Diagnostics) != "" || len(imp.ImportedResources) != 1 {
				t.Fatalf("import: %v %s", err, qaAdvErr(imp.Diagnostics))
			}
			rd, err := h.server.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{
				TypeName: h.typeName, CurrentState: imp.ImportedResources[0].State, Private: imp.ImportedResources[0].Private,
			})
			if err != nil || qaAdvErr(rd.Diagnostics) != "" {
				t.Fatalf("read: %v %s", err, qaAdvErr(rd.Diagnostics))
			}
			st, _ := rd.NewState.Unmarshal(h.resType)
			if got := attrVal(t, st, "kms_key_id"); !got.Equal(tftypes.NewValue(tftypes.String, kmsPlatformKey)) {
				t.Errorf("imported kms_key_id = %v", got)
			}
			p := h.plan(st, h.config(nil))
			if e := qaAdvErr(p.Diagnostics); e != "" {
				t.Fatalf("plan: %s", e)
			}
			for _, rp := range p.RequiresReplace {
				if rp.Equal(tftypes.NewAttributePath().WithAttributeName("kms_key_id")) {
					t.Errorf("plan after import replaces on kms_key_id")
				}
			}
			planned, _ := p.PlannedState.Unmarshal(h.resType)
			if got := attrVal(t, planned, "kms_key_id"); !got.Equal(tftypes.NewValue(tftypes.String, kmsPlatformKey)) {
				t.Errorf("planned kms_key_id after import = %v, want stored platform key", got)
			}
		})
	}
}

// Upstream 500 on create: error surfaced, no state carrying an empty key.
func TestKmsHarness_Create500NoPartialEmptyKey(t *testing.T) {
	for _, n := range kmsSpecs {
		t.Run(n, func(t *testing.T) {
			h := newKmsHarness(t, n)
			h.backend.fail = http.StatusInternalServerError
			st, diags := h.create(h.config(nil))
			if qaAdvErr(diags) == "" {
				t.Fatal("create against a 500 reported no error")
			}
			if !st.IsNull() {
				if k := attrVal(t, st, "kms_key_id"); k.IsKnown() && !k.IsNull() {
					var s string
					_ = k.As(&s)
					if strings.TrimSpace(s) == "" {
						t.Errorf("partial state has empty kms_key_id")
					}
				}
			}
		})
	}
}
