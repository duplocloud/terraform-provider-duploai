package duplocloud

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// nodeSelectorAttr mirrors the k8s_job node_selector attribute: an
// optional+computed forceNew map whose "resourcegroup" and "allocationtags"
// entries the backend stamps on every job (ApplyDuploDefaultsAsync).
func nodeSelectorAttr() []AttributeSpec {
	return []AttributeSpec{{
		Name: "node_selector", Type: "map(string)", Optional: true, Computed: true, ForceNew: true,
		RequestPath:        "spec.k8sResource.spec.template.spec.nodeSelector",
		ResponsePath:       "result.k8sResource.spec.template.spec.nodeSelector",
		FilterResponseKeys: []string{"resourcegroup", "allocationtags"},
	}}
}

func nodeSelectorType() tftypes.Object {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":            tftypes.String,
		"node_selector": tftypes.Map{ElementType: tftypes.String},
	}}
}

func nodeSelectorBase(prior map[string]tftypes.Value) tftypes.Value {
	ot := nodeSelectorType()
	ns := tftypes.NewValue(ot.AttributeTypes["node_selector"], nil)
	if prior != nil {
		ns = tftypes.NewValue(ot.AttributeTypes["node_selector"], prior)
	}
	return tftypes.NewValue(ot, map[string]tftypes.Value{
		"id":            tftypes.NewValue(tftypes.String, "job-1"),
		"node_selector": ns,
	})
}

func nodeSelectorResponse(m map[string]any) map[string]any {
	return map[string]any{"result": map[string]any{"k8sResource": map[string]any{
		"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"nodeSelector": m}}},
	}}}
}

func readNodeSelector(t *testing.T, prior map[string]tftypes.Value, server map[string]any) map[string]string {
	t.Helper()
	var diags diag.Diagnostics
	out := buildStateRaw(nodeSelectorAttr(), nodeSelectorBase(prior), nodeSelectorResponse(server),
		map[string]string{}, "job-1", true, true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	m := map[string]tftypes.Value{}
	if err := out.As(&m); err != nil {
		t.Fatal(err)
	}
	if m["node_selector"].IsNull() {
		return nil
	}
	raw := map[string]tftypes.Value{}
	if err := m["node_selector"].As(&raw); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for k, v := range raw {
		var s string
		_ = v.As(&s)
		got[k] = s
	}
	return got
}

// A user who declares the stamped key in config keeps it in state after refresh.
// Dropping it here re-opened the diff on every plan and, because node_selector
// is forceNew, planned a full replacement of the job.
func TestBuildStateRaw_ReadKeepsDeclaredFilteredKey(t *testing.T) {
	prior := map[string]tftypes.Value{
		"environment":   tftypes.NewValue(tftypes.String, "u10-dev01-env"),
		"resourcegroup": tftypes.NewValue(tftypes.String, "u10-dev01"),
	}
	got := readNodeSelector(t, prior, map[string]any{"environment": "u10-dev01-env", "resourcegroup": "u10-dev01"})
	if len(got) != 2 || got["resourcegroup"] != "u10-dev01" {
		t.Fatalf("declared resourcegroup must survive refresh, got %v", got)
	}
}

// A user who does not declare it still never sees the stamped key in state.
func TestBuildStateRaw_ReadDropsUndeclaredFilteredKey(t *testing.T) {
	prior := map[string]tftypes.Value{"environment": tftypes.NewValue(tftypes.String, "u10-dev01-env")}
	got := readNodeSelector(t, prior, map[string]any{"environment": "u10-dev01-env", "resourcegroup": "u10-dev01"})
	if len(got) != 1 || got["environment"] != "u10-dev01-env" {
		t.Fatalf("undeclared resourcegroup must stay out of state, got %v", got)
	}

	// Same when the attribute is unset entirely (null prior value).
	got = readNodeSelector(t, nil, map[string]any{"resourcegroup": "u10-dev01"})
	if len(got) != 0 {
		t.Fatalf("unset node_selector must not absorb the stamped key, got %v", got)
	}
}

// Drift on a declared key is still reported: state takes the server's value.
func TestBuildStateRaw_ReadReportsDriftOnDeclaredFilteredKey(t *testing.T) {
	prior := map[string]tftypes.Value{"resourcegroup": tftypes.NewValue(tftypes.String, "u10-dev01")}
	got := readNodeSelector(t, prior, map[string]any{"resourcegroup": "u10-dev02"})
	if got["resourcegroup"] != "u10-dev02" {
		t.Fatalf("refresh must surface the live value, got %v", got)
	}
}

// Adding your own selectors alongside the platform-stamped ones: the custom keys
// are managed by the user, every stamped key the user did not declare stays out
// of state. A job that sets allocation_tags gets "allocationtags" stamped too, so
// a config carrying only a custom key would otherwise drift on it every plan —
// and node_selector is forceNew, so that planned a replacement.
func TestBuildStateRaw_ReadKeepsCustomSelectorsAlongsideStampedOnes(t *testing.T) {
	prior := map[string]tftypes.Value{
		"nvidia.com/gpu": tftypes.NewValue(tftypes.String, "true"),
	}
	got := readNodeSelector(t, prior, map[string]any{
		"nvidia.com/gpu": "true",      // user's own selector
		"resourcegroup":  "u10-dev01", // stamped (resource group name)
		"allocationtags": "batch",     // stamped (mirrors allocation_tags)
	})
	if len(got) != 1 || got["nvidia.com/gpu"] != "true" {
		t.Fatalf("only the user's own selector belongs in state, got %v", got)
	}
}

// Mixing both: declare one platform key explicitly and add a custom one — the
// declared key round-trips, the rest of the stamped set stays hidden. Note
// "environment" is not stamped on job pods at all, so it needs no filter entry.
func TestBuildStateRaw_ReadMixesDeclaredStampedAndCustomSelectors(t *testing.T) {
	prior := map[string]tftypes.Value{
		"allocationtags": tftypes.NewValue(tftypes.String, "batch"),
		"nvidia.com/gpu": tftypes.NewValue(tftypes.String, "true"),
	}
	got := readNodeSelector(t, prior, map[string]any{
		"nvidia.com/gpu": "true",
		"allocationtags": "batch",
		"resourcegroup":  "u10-dev01",
	})
	if len(got) != 2 || got["allocationtags"] != "batch" || got["nvidia.com/gpu"] != "true" {
		t.Fatalf("declared + custom keys expected, got %v", got)
	}
}

// When the configured map is wholly unknown at plan time (e.g. built by merge()
// over a value that is itself known-after-apply), there are no declared keys to
// preserve, so the stamped keys are filtered and state omits them. The plan
// becomes fully known on the next round, at which point the declared keys are
// kept — so this converges rather than looping.
func TestBuildStateRaw_ApplyWithUnknownMapFiltersStampedKeys(t *testing.T) {
	ot := nodeSelectorType()
	base := tftypes.NewValue(ot, map[string]tftypes.Value{
		"id":            tftypes.NewValue(tftypes.String, "job-1"),
		"node_selector": tftypes.NewValue(ot.AttributeTypes["node_selector"], tftypes.UnknownValue),
	})
	var diags diag.Diagnostics
	// refreshInputs=false — the create/update path, where the plan is the base.
	out := buildStateRaw(nodeSelectorAttr(), base,
		nodeSelectorResponse(map[string]any{"nvidia.com/gpu": "true", "resourcegroup": "u10-dev01"}),
		map[string]string{}, "job-1", false, true, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	m := map[string]tftypes.Value{}
	if err := out.As(&m); err != nil {
		t.Fatal(err)
	}
	raw := map[string]tftypes.Value{}
	if err := m["node_selector"].As(&raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["resourcegroup"]; present {
		t.Errorf("an unknown plan declares nothing, so the stamped key must be filtered: %v", raw)
	}
	if _, present := raw["nvidia.com/gpu"]; !present {
		t.Errorf("unfiltered keys still come from the response: %v", raw)
	}
}

// The data source path (managedResource=false) reports the response verbatim:
// there is no config to drift against, so hiding platform-stamped keys would
// lose information for no gain.
func TestBuildStateRaw_DataSourceReportsStampedKeys(t *testing.T) {
	var diags diag.Diagnostics
	out := buildStateRaw(withoutResponseFilters(nodeSelectorAttr()), nodeSelectorBase(nil),
		nodeSelectorResponse(map[string]any{
			"nvidia.com/gpu": "true",
			"resourcegroup":  "u10-dev01",
			"allocationtags": "batch",
		}),
		map[string]string{}, "job-1", true, false, &diags)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	m := map[string]tftypes.Value{}
	if err := out.As(&m); err != nil {
		t.Fatal(err)
	}
	raw := map[string]tftypes.Value{}
	if err := m["node_selector"].As(&raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"nvidia.com/gpu", "resourcegroup", "allocationtags"} {
		if _, present := raw[k]; !present {
			t.Errorf("a data source must report %q, got %v", k, raw)
		}
	}
}

// withoutResponseFilters must reach every depth — a nested map (azure.tags) has
// to behave like a top-level one on the data source path.
func TestWithoutResponseFilters_ClearsNestedLists(t *testing.T) {
	in := []AttributeSpec{{
		Name: "azure", Type: "object", Optional: true, Computed: true,
		Attributes: []AttributeSpec{{
			Name: "tags", Type: "map(string)", Optional: true, Computed: true,
			FilterResponseKeys: []string{"duplocloud-ai-*"},
		}},
	}}
	out := withoutResponseFilters(in)
	if len(out[0].Attributes[0].FilterResponseKeys) != 0 {
		t.Errorf("nested filter not cleared: %v", out[0].Attributes[0].FilterResponseKeys)
	}
	// The input must not be mutated — the spec is shared with the managed resource.
	if len(in[0].Attributes[0].FilterResponseKeys) != 1 {
		t.Error("withoutResponseFilters must not mutate the spec it copies")
	}
}
