package duplocloud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// DUPLOAI-4756: duploai_resource_group.ssh_enabled maps the backend's
// ResourceGroupSpec.SshEnabled (spec.sshEnabled). The backend stamps false on an
// AWS create that omits it, leaves it null on non-AWS, and carries it forward
// when a full-replace PUT omits it. So the attribute is optional+computed,
// mutable in place, with no default, and a configured false must reach the wire.

// rgFake is a stateful fake of the resource-group route: POST/PUT store the
// body (as the backend does, stamping sshDefault when spec.sshEnabled is
// omitted on POST) and GET returns the stored record.
type rgFake struct {
	mu         sync.Mutex
	stored     map[string]any
	posts      []map[string]any
	puts       []map[string]any
	sshDefault any // what the backend stamps when a create omits spec.sshEnabled; nil = leave absent
	forceSsh   *any
}

func (f *rgFake) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if req.Method == http.MethodPost || req.Method == http.MethodPut {
			b, _ := io.ReadAll(req.Body)
			var body map[string]any
			_ = json.Unmarshal(b, &body)
			if body == nil {
				body = map[string]any{}
			}
			if req.Method == http.MethodPost {
				snap, _ := json.Marshal(body)
				var cp map[string]any
				_ = json.Unmarshal(snap, &cp)
				f.posts = append(f.posts, cp)
				body["id"] = "rg1"
				spec, _ := body["spec"].(map[string]any)
				if spec == nil {
					spec = map[string]any{}
					body["spec"] = spec
				}
				if _, set := spec["sshEnabled"]; !set && f.sshDefault != nil {
					spec["sshEnabled"] = f.sshDefault
				}
			} else {
				snap, _ := json.Marshal(body)
				var cp map[string]any
				_ = json.Unmarshal(snap, &cp)
				f.puts = append(f.puts, cp)
				body["id"] = "rg1"
				// full-replace PUT that omits sshEnabled is carried forward by the backend
				if old, ok := f.stored["spec"].(map[string]any); ok {
					spec, _ := body["spec"].(map[string]any)
					if spec == nil {
						spec = map[string]any{}
						body["spec"] = spec
					}
					if _, set := spec["sshEnabled"]; !set {
						if v, had := old["sshEnabled"]; had {
							spec["sshEnabled"] = v
						}
					}
				}
			}
			body["status"] = "Complete"
			f.stored = body
		}
		out := f.stored
		if out == nil {
			out = map[string]any{"id": "rg1", "status": "Complete"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
	}
}

type rgHarness struct {
	t       *testing.T
	fake    *rgFake
	server  tfprotov6.ProviderServer
	schemas *tfprotov6.GetProviderSchemaResponse
	typ     tftypes.Object
}

func newRGHarness(t *testing.T) *rgHarness {
	t.Helper()
	fake := &rgFake{}
	srv := httptest.NewServer(fake.handler(t))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	server, err := providerserver.NewProtocol6WithError(New())()
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	provType := schemas.Provider.ValueType().(tftypes.Object)
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
	cresp, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &pcfg})
	if err != nil {
		t.Fatal(err)
	}
	failOnErrors(t, "ConfigureProvider", cresp.Diagnostics)

	typ := schemas.ResourceSchemas["duploai_resource_group"].ValueType().(tftypes.Object)
	if _, ok := typ.AttributeTypes["ssh_enabled"]; !ok {
		t.Fatalf("duploai_resource_group has no ssh_enabled attribute (DUPLOAI-4756 missing behaviour)")
	}
	return &rgHarness{t: t, fake: fake, server: server, schemas: schemas, typ: typ}
}

func rgStr(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
func rgBool(v bool) tftypes.Value  { return tftypes.NewValue(tftypes.Bool, v) }

// config builds a resource config; ssh is the ssh_enabled value (nil = null/omitted).
func (h *rgHarness) config(cloud string, ssh *tftypes.Value) tftypes.Value {
	set := map[string]tftypes.Value{
		"workspace_id":   rgStr("ws9"),
		"name":           rgStr("rg-ssh"),
		"environment_id": rgStr("env-7"),
		"network_id":     rgStr("net-3"),
		"region":         rgStr("eu-west-2"),
		"cloud":          rgStr(cloud),
	}
	if ssh != nil {
		set["ssh_enabled"] = *ssh
	}
	return dsPathsObject(h.typ, set)
}

func (h *rgHarness) dyn(v tftypes.Value) *tfprotov6.DynamicValue {
	d, err := tfprotov6.NewDynamicValue(h.typ, v)
	if err != nil {
		h.t.Fatal(err)
	}
	return &d
}

func (h *rgHarness) decode(d *tfprotov6.DynamicValue) tftypes.Value {
	v, err := d.Unmarshal(h.typ)
	if err != nil {
		h.t.Fatal(err)
	}
	return v
}

func attrsOf(t *testing.T, v tftypes.Value) map[string]tftypes.Value {
	t.Helper()
	out := map[string]tftypes.Value{}
	if err := v.As(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// plan runs PlanResourceChange (prior may be null for create).
func (h *rgHarness) plan(prior, cfg tftypes.Value) (tftypes.Value, *tfprotov6.PlanResourceChangeResponse) {
	h.t.Helper()
	proposed := cfg
	if !prior.IsNull() {
		// core's ProposedNew: config wins where set, prior carries where config is null
		pa, ca := attrsOf(h.t, prior), attrsOf(h.t, cfg)
		m := map[string]tftypes.Value{}
		for n, v := range ca {
			if v.IsNull() {
				if sch := h.schemas.ResourceSchemas["duploai_resource_group"]; sch != nil {
					m[n] = pa[n]
					continue
				}
			}
			m[n] = v
		}
		proposed = tftypes.NewValue(h.typ, m)
	}
	resp, err := h.server.PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "duploai_resource_group",
		PriorState:       h.dyn(prior),
		ProposedNewState: h.dyn(proposed),
		Config:           h.dyn(cfg),
	})
	if err != nil {
		h.t.Fatal(err)
	}
	failOnErrors(h.t, "PlanResourceChange", resp.Diagnostics)
	return h.decode(resp.PlannedState), resp
}

func (h *rgHarness) apply(prior, planned, cfg tftypes.Value) tftypes.Value {
	h.t.Helper()
	resp, err := h.server.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName:       "duploai_resource_group",
		PriorState:     h.dyn(prior),
		PlannedState:   h.dyn(planned),
		Config:         h.dyn(cfg),
		PlannedPrivate: nil,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	failOnErrors(h.t, "ApplyResourceChange", resp.Diagnostics)
	return h.decode(resp.NewState)
}

func (h *rgHarness) create(cfg tftypes.Value) tftypes.Value {
	h.t.Helper()
	null := tftypes.NewValue(h.typ, nil)
	planned, _ := h.plan(null, cfg)
	return h.apply(null, planned, cfg)
}

func (h *rgHarness) refresh(state tftypes.Value) tftypes.Value {
	h.t.Helper()
	resp, err := h.server.ReadResource(context.Background(), &tfprotov6.ReadResourceRequest{
		TypeName: "duploai_resource_group", CurrentState: h.dyn(state),
	})
	if err != nil {
		h.t.Fatal(err)
	}
	failOnErrors(h.t, "ReadResource", resp.Diagnostics)
	return h.decode(resp.NewState)
}

func sshInBody(body map[string]any) (any, bool) {
	spec, _ := body["spec"].(map[string]any)
	v, ok := spec["sshEnabled"]
	return v, ok
}

// Spec shape: optional+computed bool, mutable, apiPath spec.sshEnabled, no default.
func TestResourceGroupSSHEnabled_SpecShape(t *testing.T) {
	s := customerManagedSpecs(t)["resource_group"]
	if s == nil {
		t.Fatal("no resource_group spec")
	}
	a := s.attr("ssh_enabled")
	if a == nil {
		t.Fatal("resource_group has no ssh_enabled attribute")
	}
	if a.Type != "bool" || !a.Optional || !a.Computed {
		t.Errorf("type=%q optional=%v computed=%v, want bool optional+computed", a.Type, a.Optional, a.Computed)
	}
	if a.APIPath != "spec.sshEnabled" {
		t.Errorf("apiPath=%q, want spec.sshEnabled", a.APIPath)
	}
	if a.Default != nil || a.ForceNew || a.NoSend || a.Required || a.CreateOnly || a.SendFromState {
		t.Errorf("default=%v forceNew=%v noSend=%v required=%v createOnly=%v sendFromState=%v, want none set (backend default differs per cloud; mutable in place)",
			a.Default, a.ForceNew, a.NoSend, a.Required, a.CreateOnly, a.SendFromState)
	}
	if a.Description == "" {
		t.Error("description must explain AWS-only / existing hosts keep authorized_keys")
	}
}

// A configured false must be sent (not dropped as the zero value); true likewise.
func TestResourceGroupSSHEnabled_CreateSendsConfiguredValue(t *testing.T) {
	for _, want := range []bool{false, true} {
		h := newRGHarness(t)
		v := rgBool(want)
		state := h.create(h.config("Aws", &v))
		if len(h.fake.posts) != 1 {
			t.Fatalf("want 1 POST, got %d", len(h.fake.posts))
		}
		got, ok := sshInBody(h.fake.posts[0])
		if !ok || got != want {
			t.Errorf("configured %v: create body spec.sshEnabled = %v (present=%v), want %v; body=%v", want, got, ok, want, h.fake.posts[0])
		}
		if s := attrsOf(t, state)["ssh_enabled"]; !s.Equal(rgBool(want)) {
			t.Errorf("configured %v: state ssh_enabled = %v", want, s)
		}
	}
}

// Null config: the body must not carry spec.sshEnabled; an AWS backend stamps
// false and the state shows it with no error.
func TestResourceGroupSSHEnabled_CreateOmitsWhenNull_AwsStampedFalse(t *testing.T) {
	h := newRGHarness(t)
	h.fake.sshDefault = false
	planned, _ := h.plan(tftypes.NewValue(h.typ, nil), h.config("Aws", nil))
	if s := attrsOf(t, planned)["ssh_enabled"]; s.IsKnown() {
		t.Errorf("planned ssh_enabled for an omitted optional+computed = %v, want unknown", s)
	}
	state := h.apply(tftypes.NewValue(h.typ, nil), planned, h.config("Aws", nil))
	if _, ok := sshInBody(h.fake.posts[0]); ok {
		t.Errorf("null config must not send spec.sshEnabled, body=%v", h.fake.posts[0])
	}
	if s := attrsOf(t, state)["ssh_enabled"]; !s.Equal(rgBool(false)) {
		t.Errorf("state ssh_enabled = %v, want false (backend-stamped)", s)
	}
}

// Non-AWS: backend leaves it null, state must resolve to null (a known value
// of the schema) with no error and no perpetual diff on the next plan.
func TestResourceGroupSSHEnabled_NonAwsNullNoDiff(t *testing.T) {
	for _, cloud := range []string{"Azure", "K8S_ONLY"} {
		h := newRGHarness(t)
		cfg := h.config(cloud, nil)
		state := h.create(cfg)
		if _, ok := sshInBody(h.fake.posts[0]); ok {
			t.Errorf("%s: unset ssh_enabled was sent: %v", cloud, h.fake.posts[0])
		}
		if s := attrsOf(t, state)["ssh_enabled"]; !s.IsNull() {
			t.Errorf("%s: state ssh_enabled = %v, want null", cloud, s)
		}
		refreshed := h.refresh(state)
		if s := attrsOf(t, refreshed)["ssh_enabled"]; !s.IsNull() {
			t.Errorf("%s: refreshed ssh_enabled = %v, want null (absent field)", cloud, s)
		}
		planned, _ := h.plan(refreshed, cfg)
		if !planned.Equal(refreshed) {
			t.Errorf("%s: plan after apply is not empty: ssh_enabled planned %v", cloud, attrsOf(t, planned)["ssh_enabled"])
		}
	}
}

// Non-default: a Azure group with ssh_enabled configured sends it as given.
func TestResourceGroupSSHEnabled_NonAwsConfiguredIsSent(t *testing.T) {
	h := newRGHarness(t)
	v := rgBool(false)
	h.create(h.config("Azure", &v))
	got, ok := sshInBody(h.fake.posts[0])
	if !ok || got != false {
		t.Errorf("Azure group with ssh_enabled=false: body spec.sshEnabled=%v present=%v", got, ok)
	}
}

// Update in place: flipping false->true->false is an update (no replacement),
// carries the new value on the PUT; an unchanged omitted config yields no diff.
func TestResourceGroupSSHEnabled_UpdateInPlace(t *testing.T) {
	h := newRGHarness(t)
	h.fake.sshDefault = false
	state := h.create(h.config("Aws", nil))

	// omitted config against stored false: no diff (UseStateForUnknown)
	same, resp := h.plan(state, h.config("Aws", nil))
	if !same.Equal(state) {
		t.Errorf("omitted ssh_enabled after create planned a diff: %v", attrsOf(t, same)["ssh_enabled"])
	}
	if len(resp.RequiresReplace) != 0 {
		t.Errorf("unexpected replace paths %v", resp.RequiresReplace)
	}

	for _, want := range []bool{true, false} {
		v := rgBool(want)
		cfg := h.config("Aws", &v)
		planned, resp := h.plan(state, cfg)
		if len(resp.RequiresReplace) != 0 {
			t.Fatalf("flipping ssh_enabled to %v must update in place, RequiresReplace=%v", want, resp.RequiresReplace)
		}
		before := len(h.fake.puts)
		state = h.apply(state, planned, cfg)
		if len(h.fake.puts) != before+1 {
			t.Fatalf("want one PUT for the flip to %v, got %d", want, len(h.fake.puts)-before)
		}
		got, ok := sshInBody(h.fake.puts[len(h.fake.puts)-1])
		if !ok || got != want {
			t.Errorf("update body spec.sshEnabled = %v present=%v, want %v", got, ok, want)
		}
		if s := attrsOf(t, state)["ssh_enabled"]; !s.Equal(rgBool(want)) {
			t.Errorf("state after flip = %v, want %v", s, want)
		}
	}
}

// An unrelated update (description) with ssh_enabled omitted must not wipe or
// change the stored value, and the state keeps what the backend carried forward.
func TestResourceGroupSSHEnabled_UnrelatedUpdateKeepsValue(t *testing.T) {
	h := newRGHarness(t)
	v := rgBool(true)
	state := h.create(h.config("Aws", &v))

	cfg := h.config("Aws", nil)
	ca := attrsOf(t, cfg)
	ca["description"] = rgStr("changed")
	cfg = tftypes.NewValue(h.typ, ca)
	planned, _ := h.plan(state, cfg)
	state = h.apply(state, planned, cfg)
	if s := attrsOf(t, state)["ssh_enabled"]; !s.Equal(rgBool(true)) {
		t.Errorf("ssh_enabled after unrelated update = %v, want true carried", s)
	}
}

// Read-back of true, false and absent/null from the API.
func TestResourceGroupSSHEnabled_ReadBack(t *testing.T) {
	cases := []struct {
		name   string
		stored any // JSON value placed at spec.sshEnabled; "absent" = omitted
		want   tftypes.Value
	}{
		{"true", true, rgBool(true)},
		{"false", false, rgBool(false)},
		{"absent", "absent", tftypes.NewValue(tftypes.Bool, nil)},
		{"json null", nil, tftypes.NewValue(tftypes.Bool, nil)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newRGHarness(t)
			spec := map[string]any{"cloud": "Aws", "region": "eu-west-2"}
			if c.stored != "absent" {
				spec["sshEnabled"] = c.stored
			}
			h.fake.stored = map[string]any{"id": "rg1", "name": "rg-ssh", "status": "Complete", "spec": spec}
			prior := dsPathsObject(h.typ, map[string]tftypes.Value{
				"id": rgStr("ws9/rg1"), "workspace_id": rgStr("ws9"), "name": rgStr("rg-ssh"),
			})
			got := attrsOf(t, h.refresh(prior))["ssh_enabled"]
			if !got.Equal(c.want) {
				t.Errorf("read of %s: ssh_enabled = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

// Drift: state holds a stored false while config says true -> plan shows the
// in-place update to true (config wins), and refresh reports the stored false.
func TestResourceGroupSSHEnabled_StoredFalseConfigTrue(t *testing.T) {
	h := newRGHarness(t)
	tv := rgBool(true)
	prior := h.create(h.config("Aws", &tv))
	// out-of-band change on the backend
	h.fake.mu.Lock()
	h.fake.stored["spec"].(map[string]any)["sshEnabled"] = false
	h.fake.mu.Unlock()
	refreshed := h.refresh(prior)
	if s := attrsOf(t, refreshed)["ssh_enabled"]; !s.Equal(rgBool(false)) {
		t.Fatalf("refresh must read the stored false, got %v", s)
	}
	v := rgBool(true)
	planned, resp := h.plan(refreshed, h.config("Aws", &v))
	if len(resp.RequiresReplace) != 0 {
		t.Errorf("RequiresReplace=%v, want in-place", resp.RequiresReplace)
	}
	if s := attrsOf(t, planned)["ssh_enabled"]; !s.Equal(rgBool(true)) {
		t.Errorf("planned ssh_enabled = %v, want true", s)
	}
}

// Data source exposes ssh_enabled as computed, including the null case.
func TestResourceGroupSSHEnabled_DataSource(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
		want tftypes.Value
	}{
		{"false", `{"data":{"id":"obj1","name":"g","status":"Complete","spec":{"sshEnabled":false}}}`, rgBool(false)},
		{"true", `{"data":{"id":"obj1","name":"g","status":"Complete","spec":{"sshEnabled":true}}}`, rgBool(true)},
		{"null", `{"data":{"id":"obj1","name":"g","status":"Complete","spec":{}}}`, tftypes.NewValue(tftypes.Bool, nil)},
	} {
		t.Run(c.name, func(t *testing.T) {
			server, schemas := dsPathsServer(t, c.body)
			sch := schemas.DataSourceSchemas["duploai_resource_group"]
			if sch == nil {
				t.Fatal("no data source duploai_resource_group")
			}
			if _, ok := sch.ValueType().(tftypes.Object).AttributeTypes["ssh_enabled"]; !ok {
				t.Fatal("data source duploai_resource_group has no ssh_enabled")
			}
			ds := readDataSource(t, server, schemas, "duploai_resource_group")
			if !ds["ssh_enabled"].Equal(c.want) {
				t.Errorf("data source ssh_enabled = %v, want %v", ds["ssh_enabled"], c.want)
			}
		})
	}
}

// Compat: state written by v0.7.0 (no ssh_enabled key) upgrades cleanly and
// the new attribute is purely additive (optional+computed, never required).
func TestResourceGroupSSHEnabled_OldStateUpgradesWithoutDiff(t *testing.T) {
	h := newRGHarness(t)
	raw := `{"id":"ws9/rg1","workspace_id":"ws9","name":"rg-ssh","environment_id":"env-7","network_id":"net-3","region":"eu-west-2","cloud":"Aws","description":""}`
	resp, err := h.server.UpgradeResourceState(context.Background(), &tfprotov6.UpgradeResourceStateRequest{
		TypeName: "duploai_resource_group", Version: 0, RawState: &tfprotov6.RawState{JSON: []byte(raw)},
	})
	if err != nil {
		t.Fatal(err)
	}
	failOnErrors(t, "UpgradeResourceState", resp.Diagnostics)
	upgraded := h.decode(resp.UpgradedState)
	if s := attrsOf(t, upgraded)["ssh_enabled"]; !s.IsNull() {
		t.Errorf("upgraded ssh_enabled = %v, want null", s)
	}
	// plan with the old config (no ssh_enabled) against the upgraded state: no ssh_enabled diff.
	cfg := h.config("Aws", nil)
	planned, _ := h.plan(upgraded, cfg)
	if s := attrsOf(t, planned)["ssh_enabled"]; !s.IsNull() {
		t.Errorf("old state + old config plans ssh_enabled = %v, want null (no diff, no unknown)", s)
	}
}

// Wrong type in config is rejected by the schema (bool attribute).
func TestResourceGroupSSHEnabled_WrongTypeRejected(t *testing.T) {
	h := newRGHarness(t)
	if _, ok := h.typ.AttributeTypes["ssh_enabled"].(interface{ Is(tftypes.Type) bool }); ok {
		if !h.typ.AttributeTypes["ssh_enabled"].Is(tftypes.Bool) {
			t.Errorf("ssh_enabled type = %v, want bool", h.typ.AttributeTypes["ssh_enabled"])
		}
	}
}
