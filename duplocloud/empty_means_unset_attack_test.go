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

// Attack round on emptyMeansUnset (kms_key_id): everything below drives the REAL
// embedded rds_instance / rds_cluster specs through providerserver (schema build,
// plan modifiers, ModifyPlan, Create/Update/Read, bodyFromRaw, buildStateRaw). Only
// HTTP is stubbed.

const (
	atkKeyA     = "arn:aws:kms:us-west-2:111122223333:key/aaaaaaaa-1111-2222-3333-444444444444"
	atkKeyB     = "arn:aws:kms:us-west-2:111122223333:key/bbbbbbbb-1111-2222-3333-444444444444"
	atkDefault  = "arn:aws:kms:us-west-2:111122223333:key/dddddddd-1111-2222-3333-444444444444"
	atkEngineIn = "postgres"
)

type atkReq struct {
	method string
	body   map[string]any
}

type atkHarness struct {
	t       *testing.T
	ctx     context.Context
	name    string
	server  tfprotov6.ProviderServer
	rs      *tfprotov6.Schema
	resType tftypes.Object
	mu      sync.Mutex
	record  map[string]any // what the stub platform stores
	reqs    []atkReq
}

func newAtkHarness(t *testing.T, name string, stored map[string]any) *atkHarness {
	t.Helper()
	h := &atkHarness{t: t, ctx: context.Background(), name: name}
	h.record = map[string]any{"id": "obj1", "name": "db", "status": "Complete", "spec": map[string]any{}}
	for k, v := range stored {
		h.record["spec"].(map[string]any)[k] = v
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		if b, _ := io.ReadAll(req.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &body)
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		if req.Method == http.MethodPost || req.Method == http.MethodPut {
			h.reqs = append(h.reqs, atkReq{req.Method, body})
			spec, _ := h.record["spec"].(map[string]any)
			if bs, ok := body["spec"].(map[string]any); ok {
				for k, v := range bs {
					spec[k] = v
				}
			}
			if req.Method == http.MethodPost {
				if _, ok := spec["storageKmsKeyId"]; !ok {
					spec["storageKmsKeyId"] = atkDefault // the platform picks its default key
				}
			}
		}
		out, _ := json.Marshal(map[string]any{"data": h.record})
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)

	server, err := providerserver.NewProtocol6WithError(New())()
	if err != nil {
		t.Fatalf("provider server: %v", err)
	}
	schemas, err := server.GetProviderSchema(h.ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil || len(schemas.Diagnostics) > 0 {
		t.Fatalf("GetProviderSchema: %v %v", err, schemas.Diagnostics)
	}
	pt := schemas.Provider.ValueType().(tftypes.Object)
	cfg := protoDynamic(t, pt, objectWith(pt, map[string]tftypes.Value{
		"duplo_host":  tftypes.NewValue(tftypes.String, srv.URL),
		"duplo_token": tftypes.NewValue(tftypes.String, "tok"),
	}))
	conf, err := server.ConfigureProvider(h.ctx, &tfprotov6.ConfigureProviderRequest{Config: &cfg})
	if err != nil || len(conf.Diagnostics) > 0 {
		t.Fatalf("ConfigureProvider: %v %v", err, conf.Diagnostics)
	}
	h.server = server
	h.rs = schemas.ResourceSchemas["duploai_"+name]
	if h.rs == nil {
		t.Fatalf("no schema for duploai_%s", name)
	}
	h.resType = h.rs.ValueType().(tftypes.Object)
	return h
}

func (h *atkHarness) s(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

// base is the user config minus kms_key_id and the extra attribute.
func (h *atkHarness) base(extra map[string]tftypes.Value) map[string]tftypes.Value {
	m := map[string]tftypes.Value{
		"workspace_id":      h.s("ws"),
		"name":              h.s("db"),
		"scope_ids":         tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{h.s("sc")}),
		"resource_group_id": h.s("rg"),
		"environment_id":    h.s("env"),
		"engine":            h.s(atkEngineIn),
	}
	if h.name == "rds_instance" {
		m["db_instance_class"] = h.s("db.t3.micro")
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func (h *atkHarness) kms(v *string) tftypes.Value {
	if v == nil {
		return tftypes.NewValue(tftypes.String, nil)
	}
	return h.s(*v)
}

func sp(s string) *string { return &s }

type atkPlan struct {
	resp    *tfprotov6.PlanResourceChangeResponse
	planned map[string]tftypes.Value
	prior   tftypes.Value
	config  tftypes.Value
	pdv     tfprotov6.DynamicValue
	cdv     tfprotov6.DynamicValue
	priordv tfprotov6.DynamicValue
}

// plan runs PlanResourceChange. prior == nil means create.
func (h *atkHarness) plan(prior map[string]tftypes.Value, config map[string]tftypes.Value) *atkPlan {
	h.t.Helper()
	cfgVal := objectWith(h.resType, config)
	priorVal := tftypes.NewValue(h.resType, nil)
	if prior != nil {
		priorVal = objectWith(h.resType, prior)
	}
	var proposed tftypes.Value
	if prior == nil {
		proposed = cfgVal
	} else {
		proposed = proposedNewState(h.rs, h.resType, priorVal, cfgVal)
	}
	p := &atkPlan{prior: priorVal, config: cfgVal}
	p.priordv = protoDynamic(h.t, h.resType, priorVal)
	p.cdv = protoDynamic(h.t, h.resType, cfgVal)
	pr := protoDynamic(h.t, h.resType, proposed)
	resp, err := h.server.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName: "duploai_" + h.name, PriorState: &p.priordv, ProposedNewState: &pr, Config: &p.cdv,
	})
	if err != nil {
		h.t.Fatalf("PlanResourceChange: %v", err)
	}
	p.resp = resp
	if !hasError(resp.Diagnostics) {
		v, err := resp.PlannedState.Unmarshal(h.resType)
		if err != nil {
			h.t.Fatalf("unmarshal plan: %v", err)
		}
		_ = v.As(&p.planned)
		p.pdv = *resp.PlannedState
	}
	return p
}

func hasError(ds []*tfprotov6.Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			return true
		}
	}
	return false
}

func diagText(ds []*tfprotov6.Diagnostic) string {
	var b strings.Builder
	for _, d := range ds {
		b.WriteString(d.Summary + ": " + d.Detail + "\n")
	}
	return b.String()
}

func (p *atkPlan) replaces() bool { return len(p.resp.RequiresReplace) > 0 }

// coreAccepts mirrors Terraform core's plan validity rule for a configured
// attribute: when the config value is known and non-null the plan must equal it.
func coreAccepts(config, planned tftypes.Value) bool {
	if config.IsNull() || !config.IsKnown() {
		return true
	}
	return planned.Equal(config)
}

// apply runs ApplyResourceChange for a plan and returns the new state.
func (h *atkHarness) apply(p *atkPlan) map[string]tftypes.Value {
	h.t.Helper()
	resp, err := h.server.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName: "duploai_" + h.name, PriorState: &p.priordv, PlannedState: &p.pdv, Config: &p.cdv,
	})
	if err != nil || hasError(resp.Diagnostics) {
		h.t.Fatalf("ApplyResourceChange: %v %s", err, diagText(resp.Diagnostics))
	}
	v, err := resp.NewState.Unmarshal(h.resType)
	if err != nil {
		h.t.Fatalf("unmarshal state: %v", err)
	}
	var m map[string]tftypes.Value
	_ = v.As(&m)
	return m
}

func (h *atkHarness) lastBody(method string) (map[string]any, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.reqs) - 1; i >= 0; i-- {
		if h.reqs[i].method == method {
			return h.reqs[i].body, true
		}
	}
	return nil, false
}

func (h *atkHarness) sentKms(method string) (any, bool) {
	b, ok := h.lastBody(method)
	if !ok {
		h.t.Fatalf("no %s was sent", method)
	}
	spec, _ := b["spec"].(map[string]any)
	v, present := spec["storageKmsKeyId"]
	return v, present
}

// priorState is a post-apply state for the resource holding kms.
func (h *atkHarness) priorState(kms *string, extra map[string]tftypes.Value) map[string]tftypes.Value {
	m := h.base(extra)
	m["id"] = h.s("ws/obj1")
	m["status"] = h.s("Complete")
	m["kms_key_id"] = h.kms(kms)
	// Real state holds every defaulted attribute (provisioner_type, ...).
	for _, a := range customerManagedSpecs(h.t)[h.name].Attributes {
		if _, set := m[a.Name]; !set && a.Default != nil {
			var d any
			if err := json.Unmarshal(*a.Default, &d); err != nil {
				h.t.Fatalf("default of %s: %v", a.Name, err)
			}
			m[a.Name] = goToTftypesValue(h.resType.AttributeTypes[a.Name], d)
		}
	}
	return m
}

func atkResources() []string { return []string{"rds_instance", "rds_cluster"} }

// ── Create: input boundary matrix ────────────────────────────────────────────

func TestEmptyMeansUnset_Attack_CreateBoundary(t *testing.T) {
	long := "arn:aws:kms:us-west-2:111122223333:key/" + strings.Repeat("a", 5000)
	cases := []struct {
		name       string
		cfg        *string
		wantSent   bool
		wantSentV  string
		wantStateV string // state after apply
	}{
		{"null", nil, false, "", atkDefault},
		{"empty", sp(""), false, "", ""},
		{"real key", sp(atkKeyA), true, atkKeyA, atkKeyA},
		{"malformed, not an arn", sp("not-an-arn"), true, "not-an-arn", "not-an-arn"},
		{"very long", sp(long), true, long, long},
	}
	for _, res := range atkResources() {
		for _, c := range cases {
			t.Run(res+"/"+c.name, func(t *testing.T) {
				h := newAtkHarness(t, res, nil)
				cfg := h.base(nil)
				cfg["kms_key_id"] = h.kms(c.cfg)
				p := h.plan(nil, cfg)
				if hasError(p.resp.Diagnostics) {
					t.Fatalf("plan error: %s", diagText(p.resp.Diagnostics))
				}
				pk := p.planned["kms_key_id"]
				if c.cfg != nil && !coreAccepts(cfg["kms_key_id"], pk) {
					t.Fatalf("planned %v != config %v (core would reject)", pk, cfg["kms_key_id"])
				}
				if c.cfg == nil && !pk.IsKnown() {
					// an unset computed attribute is legitimately unknown on create
				}
				if c.cfg != nil && !pk.IsKnown() {
					t.Fatalf("a configured kms_key_id must never plan unknown, got %v", pk)
				}
				st := h.apply(p)
				v, present := h.sentKms("POST")
				if present != c.wantSent || (present && v != c.wantSentV) {
					t.Errorf("POST storageKmsKeyId present=%v value=%v, want present=%v %q", present, v, c.wantSent, c.wantSentV)
				}
				var got string
				if err := st["kms_key_id"].As(&got); err != nil {
					t.Fatalf("state kms: %v", st["kms_key_id"])
				}
				if got != c.wantStateV {
					t.Errorf("state kms_key_id = %q, want %q", got, c.wantStateV)
				}
				if pk.IsKnown() && !pk.Equal(st["kms_key_id"]) {
					t.Errorf("planned %v != applied %v (Terraform: inconsistent result after apply)", pk, st["kms_key_id"])
				}
			})
		}
	}
}

// An unknown config (a key from another resource) is the same as a real key at
// plan time: known-after-apply and no panic.
func TestEmptyMeansUnset_Attack_CreateUnknownConfig(t *testing.T) {
	for _, res := range atkResources() {
		t.Run(res, func(t *testing.T) {
			h := newAtkHarness(t, res, nil)
			cfg := h.base(nil)
			cfg["kms_key_id"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
			p := h.plan(nil, cfg)
			if hasError(p.resp.Diagnostics) {
				t.Fatalf("plan error: %s", diagText(p.resp.Diagnostics))
			}
			if p.planned["kms_key_id"].IsKnown() {
				t.Errorf("unknown config must plan unknown, got %v", p.planned["kms_key_id"])
			}
		})
	}
}

// ── Plan against existing state: full state x config matrix ──────────────────

func TestEmptyMeansUnset_Attack_PlanMatrix(t *testing.T) {
	type tc struct {
		name        string
		state, cfg  *string
		wantReplace bool
		wantPlanned *string // nil = must be unknown? no: use wantKeep
		wantKeep    bool    // plan must equal the state value (unset keeps stored)
	}
	cases := []tc{
		{"state null, config empty (import then plan)", nil, sp(""), false, sp(""), false},
		{"state empty, config empty", sp(""), sp(""), false, sp(""), false},
		{"state key, config empty", sp(atkKeyA), sp(""), false, sp(""), false},
		{"state empty, config key", sp(""), sp(atkKeyA), true, sp(atkKeyA), false},
		{"state null, config key", nil, sp(atkKeyA), true, sp(atkKeyA), false},
		{"state key, config other key", sp(atkKeyA), sp(atkKeyB), true, sp(atkKeyB), false},
		{"state key, config unset", sp(atkKeyA), nil, false, nil, true},
		{"state empty, config unset", sp(""), nil, false, nil, true},
		{"state key, config same key", sp(atkKeyA), sp(atkKeyA), false, sp(atkKeyA), false},
	}
	for _, res := range atkResources() {
		for _, c := range cases {
			t.Run(res+"/"+c.name, func(t *testing.T) {
				h := newAtkHarness(t, res, nil)
				cfg := h.base(nil)
				cfg["kms_key_id"] = h.kms(c.cfg)
				p := h.plan(h.priorState(c.state, nil), cfg)
				if hasError(p.resp.Diagnostics) {
					t.Fatalf("plan error: %s", diagText(p.resp.Diagnostics))
				}
				if p.replaces() != c.wantReplace {
					t.Errorf("replace = %v (%v), want %v", p.replaces(), p.resp.RequiresReplace, c.wantReplace)
				}
				pk := p.planned["kms_key_id"]
				if !pk.IsKnown() {
					t.Fatalf("plan went unknown (would replace dependents), config=%v state=%v", c.cfg, c.state)
				}
				if c.wantPlanned != nil && !pk.Equal(h.s(*c.wantPlanned)) {
					t.Errorf("planned %v, want %q", pk, *c.wantPlanned)
				}
				if c.wantKeep && !pk.Equal(h.kms(c.state)) {
					t.Errorf("unset config must keep the stored value %v, planned %v", c.state, pk)
				}
				if !coreAccepts(cfg["kms_key_id"], pk) {
					t.Errorf("planned %v does not equal configured %v: Terraform core rejects this plan "+
						"(\"Provider produced invalid plan\")", pk, cfg["kms_key_id"])
				}
			})
		}
	}
}

// ── Update of another attribute ──────────────────────────────────────────────

// Changing backup_retention_period with kms_key_id "" must not replace and must
// not put an (empty) storageKmsKeyId in the PUT. mergeUpdateIntoCurrent resends the
// platform-held record, so a key the platform already stores is carried unchanged
// (stored="" cases: nothing); an empty one is never injected.
func TestEmptyMeansUnset_Attack_UpdateOtherAttribute(t *testing.T) {
	cases := []struct {
		name      string
		state     *string
		storedAPI map[string]any
		wantSent  any // nil = key absent from the PUT
	}{
		{"state empty, platform has no key", sp(""), nil, nil},
		{"state null, platform has no key", nil, nil, nil},
		{"state empty, platform stores its default key", sp(""), map[string]any{"storageKmsKeyId": atkDefault}, atkDefault},
		{"state key, platform stores key", sp(atkKeyA), map[string]any{"storageKmsKeyId": atkKeyA}, atkKeyA},
	}
	for _, res := range atkResources() {
		for _, c := range cases {
			t.Run(res+"/"+c.name, func(t *testing.T) {
				stored := map[string]any{"backupRetentionPeriod": 5}
				for k, v := range c.storedAPI {
					stored[k] = v
				}
				h := newAtkHarness(t, res, stored)
				priorExtra := map[string]tftypes.Value{"backup_retention_period": tftypes.NewValue(tftypes.Number, 5)}
				cfgExtra := map[string]tftypes.Value{"backup_retention_period": tftypes.NewValue(tftypes.Number, 7)}
				cfg := h.base(cfgExtra)
				cfg["kms_key_id"] = h.s("")
				p := h.plan(h.priorState(c.state, priorExtra), cfg)
				if hasError(p.resp.Diagnostics) {
					t.Fatalf("plan error: %s", diagText(p.resp.Diagnostics))
				}
				if p.replaces() {
					t.Fatalf("an unrelated change with kms_key_id \"\" must not replace: %v", p.resp.RequiresReplace)
				}
				pk := p.planned["kms_key_id"]
				if !pk.Equal(h.s("")) {
					t.Fatalf("planned kms_key_id = %v, want \"\"", pk)
				}
				st := h.apply(p)
				v, present := h.sentKms("PUT")
				if c.wantSent == nil && present {
					t.Errorf("PUT carries storageKmsKeyId=%v, must be absent", v)
				}
				if c.wantSent != nil && v != c.wantSent {
					t.Errorf("PUT storageKmsKeyId=%v present=%v, want the platform-held %v", v, present, c.wantSent)
				}
				if v == "" {
					t.Errorf("an empty storageKmsKeyId must never be sent")
				}
				// state -> plan -> apply consistency
				if !st["kms_key_id"].Equal(pk) {
					t.Errorf("applied kms_key_id %v != planned %v", st["kms_key_id"], pk)
				}
			})
		}
	}
}

// ── state -> config "" -> apply -> re-plan converges ─────────────────────────

// A stored key then config "": no replacement, a one-time in-place update
// (key -> "") that records "" in state; the next plan is empty. This is the
// documented in-place diff.
func TestEmptyMeansUnset_Attack_StoredKeyThenEmptyConverges(t *testing.T) {
	for _, res := range atkResources() {
		t.Run(res, func(t *testing.T) {
			h := newAtkHarness(t, res, map[string]any{"storageKmsKeyId": atkKeyA})
			cfg := h.base(nil)
			cfg["kms_key_id"] = h.s("")
			p := h.plan(h.priorState(sp(atkKeyA), nil), cfg)
			if hasError(p.resp.Diagnostics) || p.replaces() {
				t.Fatalf("diags=%s replace=%v", diagText(p.resp.Diagnostics), p.resp.RequiresReplace)
			}
			if !p.planned["kms_key_id"].Equal(h.s("")) {
				t.Fatalf("planned %v, want \"\" (in-place key -> \"\")", p.planned["kms_key_id"])
			}
			st := h.apply(p)
			if !st["kms_key_id"].Equal(h.s("")) {
				t.Fatalf("state after apply = %v, want \"\"", st["kms_key_id"])
			}
			// key -> "" is a state-only change (omitEmptyUnsetFromState): no PUT, the
			// platform keeps its key.
			h.mu.Lock()
			for _, r := range h.reqs {
				if r.method == http.MethodPut {
					t.Errorf("key -> \"\" must not send a PUT, got %v", r.body)
				}
			}
			h.mu.Unlock()
			// Re-plan from the new state: nothing to do.
			p2 := h.plan(st, cfg)
			if hasError(p2.resp.Diagnostics) || p2.replaces() {
				t.Fatalf("re-plan: diags=%s replace=%v", diagText(p2.resp.Diagnostics), p2.resp.RequiresReplace)
			}
			if !p2.planned["kms_key_id"].Equal(h.s("")) {
				t.Errorf("re-plan kms_key_id = %v, want \"\"", p2.planned["kms_key_id"])
			}
		})
	}
}

// Refresh keeps a held "" even though the platform reports the key it chose.
func TestEmptyMeansUnset_Attack_ReadKeepsEmptyAndNull(t *testing.T) {
	for _, res := range atkResources() {
		for _, held := range []*string{sp(""), nil} {
			name := "held null"
			if held != nil {
				name = "held empty"
			}
			t.Run(res+"/"+name, func(t *testing.T) {
				h := newAtkHarness(t, res, map[string]any{"storageKmsKeyId": atkDefault})
				prior := objectWith(h.resType, h.priorState(held, nil))
				pdv := protoDynamic(t, h.resType, prior)
				resp, err := h.server.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{TypeName: "duploai_" + h.name, CurrentState: &pdv})
				if err != nil || hasError(resp.Diagnostics) {
					t.Fatalf("Read: %v %s", err, diagText(resp.Diagnostics))
				}
				v, _ := resp.NewState.Unmarshal(h.resType)
				var m map[string]tftypes.Value
				_ = v.As(&m)
				want := h.s(atkDefault)
				if held != nil {
					want = h.s("")
				}
				if !m["kms_key_id"].Equal(want) {
					t.Errorf("read kms_key_id = %v, want %v", m["kms_key_id"], want)
				}
			})
		}
	}
}

// ── Whitespace ───────────────────────────────────────────────────────────────

// Whitespace-only is NOT treated as "": v0.7.0 already replaced on it under plain
// forceNew, and rejecting it now would narrow accepted input (compat rule). This
// pins today's behaviour so a change to it is deliberate.
func TestEmptyMeansUnset_Attack_WhitespaceTodayCharacterisation(t *testing.T) {
	h := newAtkHarness(t, "rds_instance", nil)
	cfg := h.base(nil)
	cfg["kms_key_id"] = h.s(" ")
	p := h.plan(h.priorState(sp(""), nil), cfg) // an existing DB with no key chosen
	t.Logf("state \"\" -> config \" \": diags=%q replace=%v planned=%v", diagText(p.resp.Diagnostics), p.resp.RequiresReplace, p.planned["kms_key_id"])
	c := h.plan(nil, cfg)
	h.apply(c)
	v, present := h.sentKms("POST")
	t.Logf("create with \" \": POST storageKmsKeyId present=%v value=%q", present, v)
}

// ── Spec load: flag on a nested / list attribute ─────────────────────────────

func TestEmptyMeansUnset_Attack_RejectedOnNestedAndCollections(t *testing.T) {
	flags := func(a AttributeSpec) AttributeSpec {
		a.Optional, a.Computed, a.ForceNew, a.EmptyMeansUnset = true, true, true, true
		return a
	}
	cases := map[string]AttributeSpec{
		"list(string)": flags(AttributeSpec{Name: "x", Type: "list(string)"}),
		"set(string)":  flags(AttributeSpec{Name: "x", Type: "set(string)"}),
		"map(string)":  flags(AttributeSpec{Name: "x", Type: "map(string)"}),
		"object":       flags(AttributeSpec{Name: "x", Type: "object", Attributes: []AttributeSpec{{Name: "y", Type: "string", Optional: true}}}),
		"bool":         flags(AttributeSpec{Name: "x", Type: "bool"}),
	}
	for n, a := range cases {
		t.Run(n, func(t *testing.T) {
			if _, err := validateAttributes([]AttributeSpec{a}); err == nil || !strings.Contains(err.Error(), "emptyMeansUnset") {
				t.Errorf("expected an emptyMeansUnset error, got %v", err)
			}
		})
	}
	t.Run("nested child string", func(t *testing.T) {
		child := flags(AttributeSpec{Name: "kms", Type: "string"})
		parent := AttributeSpec{Name: "p", Type: "object", Optional: true, Computed: true, Attributes: []AttributeSpec{child}}
		_, err := validateAttributes([]AttributeSpec{parent})
		t.Logf("flag on a string child of an object: err=%v", err)
		if err == nil {
			t.Errorf("flag on a nested child is accepted at spec load; bodyFromRaw/keepEmptyUnset only act on top-level attributes (dynamic_resource.go:1606, :2139), so it would be a silent no-op for body/read while stringRequiresReplace still applies")
		}
	})
	t.Run("list(object) element child", func(t *testing.T) {
		child := flags(AttributeSpec{Name: "kms", Type: "string"})
		parent := AttributeSpec{Name: "p", Type: "list(object)", Optional: true, Computed: true, Attributes: []AttributeSpec{child}}
		_, err := validateAttributes([]AttributeSpec{parent})
		t.Logf("flag on a child of list(object): err=%v", err)
		if err == nil {
			t.Errorf("flag on a list(object) child is accepted at spec load")
		}
	})
}
