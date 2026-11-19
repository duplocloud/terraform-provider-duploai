package duplocloud

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// qaAdvK8sBackend mimics the backend's K8sObjectEnricher.StampPlatformLabels
// (BE origin/dev): user-supplied duplocloud.ai/* labels are STRIPPED, then
// the platform's duplocloud.ai/{workspace,...} labels are stamped. The live
// object (result.k8sResource) is what every GET returns.
type qaAdvK8sBackend struct {
	mu     sync.Mutex
	labels map[string]any
}

func (b *qaAdvK8sBackend) handle(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		in, _ := extractPath(body, strings.Split("spec.k8sResource.metadata.labels", ".")).(map[string]any)
		b.labels = map[string]any{}
		for k, v := range in {
			if !strings.HasPrefix(k, "duplocloud.ai/") {
				b.labels[k] = v
			}
		}
		b.labels["duplocloud.ai/workspace"] = "ws1"
		b.labels["duplocloud.ai/resourcetype"] = "K8sConfigMap"
	}
	resp := map[string]any{"data": map[string]any{
		"id": "cm1", "name": "cm1", "status": "Complete",
		"spec":   map[string]any{"resourceGroupId": "rg1", "environmentId": "env1", "namespaceName": "ns"},
		"result": map[string]any{"k8sResource": map[string]any{"metadata": map[string]any{"labels": b.labels}}},
	}}
	out, _ := json.Marshal(resp)
	_, _ = w.Write(out)
}

// adv-qa F-6. A configured label under the platform-reserved duplocloud.ai/
// prefix is stripped by the backend. The provider must not plan a value it
// cannot deliver: Terraform core fails the apply with "Provider produced
// inconsistent result after apply" when the new state differs from the plan.
// Acceptable outcomes: a plan-time error/warning naming the reserved prefix,
// or new state == planned labels.
func TestQAAdv_K8sConfigMap_ReservedLabelPrefix(t *testing.T) {
	be := &qaAdvK8sBackend{}
	h := newQAAdvHarness(t, be.handle)
	const name = "duploai_k8s_config_map"
	labels := tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{
		"app":                tftypes.NewValue(tftypes.String, "a"),
		"duplocloud.ai/team": tftypes.NewValue(tftypes.String, "payments"),
	})
	cfgVal := h.obj(name, map[string]tftypes.Value{
		"workspace_id":      tftypes.NewValue(tftypes.String, "ws1"),
		"name":              tftypes.NewValue(tftypes.String, "cm1"),
		"resource_group_id": tftypes.NewValue(tftypes.String, "rg1"),
		"environment_id":    tftypes.NewValue(tftypes.String, "env1"),
		"namespace_name":    tftypes.NewValue(tftypes.String, "ns"),
		"labels":            labels,
	})
	cfg := h.dv(name, cfgVal)
	prior := h.dv(name, tftypes.NewValue(h.resType(name), nil))

	vr, err := h.server.ValidateResourceConfig(h.ctx, &tfprotov6.ValidateResourceConfigRequest{TypeName: name, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if qaAdvErr(vr.Diagnostics) != "" || len(qaAdvWarn(vr.Diagnostics)) > 0 {
		t.Logf("validate: err=%q warn=%v", qaAdvErr(vr.Diagnostics), qaAdvWarn(vr.Diagnostics))
		return // flagged at validate time: fine
	}
	pr, err := h.server.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName: name, PriorState: prior, ProposedNewState: cfg, Config: cfg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if qaAdvErr(pr.Diagnostics) != "" || len(qaAdvWarn(pr.Diagnostics)) > 0 {
		t.Logf("plan: err=%q warn=%v", qaAdvErr(pr.Diagnostics), qaAdvWarn(pr.Diagnostics))
		return // flagged at plan time: fine
	}
	ar, err := h.server.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName: name, PriorState: prior, PlannedState: pr.PlannedState, Config: cfg, PlannedPrivate: pr.PlannedPrivate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if e := qaAdvErr(ar.Diagnostics); e != "" {
		t.Fatalf("apply: %s", e)
	}
	planned, err := pr.PlannedState.Unmarshal(h.resType(name))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ar.NewState.Unmarshal(h.resType(name))
	if err != nil {
		t.Fatal(err)
	}
	var pm, gm map[string]tftypes.Value
	_ = planned.As(&pm)
	_ = got.As(&gm)
	if !pm["labels"].Equal(gm["labels"]) {
		t.Errorf("planned labels %v but new state labels %v: Terraform core will fail with "+
			"'Provider produced inconsistent result after apply' (backend strips duplocloud.ai/*); "+
			"want a validate/plan diagnostic for the reserved prefix", pm["labels"], gm["labels"])
	}

	// Refresh, then plan again with the same config: must converge (no diff).
	rr, err := h.server.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{
		TypeName: name, CurrentState: ar.NewState, Private: ar.Private, CurrentIdentity: ar.NewIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if e := qaAdvErr(rr.Diagnostics); e != "" {
		t.Fatalf("read: %s", e)
	}
	refreshed, _ := rr.NewState.Unmarshal(h.resType(name))
	var rm map[string]tftypes.Value
	_ = refreshed.As(&rm)
	pr2, err := h.server.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName: name, PriorState: rr.NewState, ProposedNewState: qaAdvProposed(t, h, name, refreshed, cfgVal),
		Config: cfg, PriorPrivate: rr.Private, PriorIdentity: rr.NewIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if e := qaAdvErr(pr2.Diagnostics); e != "" {
		t.Fatalf("second plan: %s", e)
	}
	p2, _ := pr2.PlannedState.Unmarshal(h.resType(name))
	if !p2.Equal(refreshed) {
		var p2m map[string]tftypes.Value
		_ = p2.As(&p2m)
		t.Errorf("perpetual diff: refreshed labels %v, second plan labels %v (backend strips duplocloud.ai/*; "+
			"every apply re-sends it, never converges); want a validate/plan diagnostic for the reserved prefix",
			rm["labels"], p2m["labels"])
	}
}

// qaAdvProposed mimics Terraform core's ProposedNewState for an update: config
// values for settable attributes, prior values for computed-only ones and for
// optional+computed attributes left null in config.
func qaAdvProposed(t *testing.T, h *qaAdvHarness, name string, prior, config tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()
	sch := h.schemas.ResourceSchemas[name]
	var pm, cm map[string]tftypes.Value
	_ = prior.As(&pm)
	_ = config.As(&cm)
	out := map[string]tftypes.Value{}
	for _, a := range sch.Block.Attributes {
		switch {
		case a.Computed && !a.Optional && !a.Required:
			out[a.Name] = pm[a.Name]
		case a.Computed && cm[a.Name].IsNull():
			out[a.Name] = pm[a.Name]
		default:
			out[a.Name] = cm[a.Name]
		}
	}
	for n := range pm {
		if _, ok := out[n]; !ok {
			out[n] = pm[n] // nested blocks (timeouts)
		}
	}
	return h.dv(name, tftypes.NewValue(h.resType(name), out))
}
