package duplocloud

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// A change the platform can't apply (createOnly, or any field of a resource
// with no update operation) gets a plan warning; real updates, unchanged
// values and creates get none. Warnings never fail the plan.
func TestWarnIgnoredChanges(t *testing.T) {
	attrs := []AttributeSpec{
		{Name: "workspace_id", Type: "string", Required: true, ForceNew: true},
		{Name: "name", Type: "string", Required: true, ForceNew: true, APIPath: "name"},
		{Name: "description", Type: "string", Optional: true, APIPath: "description"},
		{Name: "tags", Type: "string", Optional: true, CreateOnly: true, RequestPath: "spec.tags"},
	}
	ctx := context.Background()
	run := func(ep duplosdk.Endpoint, state map[string]string, plan map[string]string) []string {
		r := &dynamicResource{spec: ResourceSpec{Name: "thing", IDPath: "id", Attributes: attrs}, endpoint: ep}
		var sr resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sr)
		typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
		mk := func(v map[string]string) tftypes.Value {
			if v == nil {
				return tftypes.NewValue(typ, nil)
			}
			vals := map[string]tftypes.Value{}
			for n, at := range typ.AttributeTypes {
				vals[n] = tftypes.NewValue(at, nil)
			}
			for k, s := range v {
				vals[k] = tftypes.NewValue(tftypes.String, s)
			}
			return tftypes.NewValue(typ, vals)
		}
		req := resource.ModifyPlanRequest{
			State:  tfsdk.State{Schema: sr.Schema, Raw: mk(state)},
			Plan:   tfsdk.Plan{Schema: sr.Schema, Raw: mk(plan)},
			Config: tfsdk.Config{Schema: sr.Schema, Raw: mk(plan)},
		}
		resp := &resource.ModifyPlanResponse{Plan: req.Plan}
		r.ModifyPlan(ctx, req, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("warnings must never be errors: %v", resp.Diagnostics)
		}
		var got []string
		for _, d := range resp.Diagnostics.Warnings() {
			got = append(got, d.Summary())
		}
		return got
	}
	updatable := duplosdk.Endpoint{UriBase: "/x/{workspace_id}", Update: duplosdk.Operation{Verb: http.MethodPut}}
	immutable := duplosdk.Endpoint{UriBase: "/x/{workspace_id}"}
	base := map[string]string{"id": "ws/1", "workspace_id": "ws", "name": "n", "description": "d", "tags": "a"}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for a, b := range base {
			m[a] = b
		}
		m[k] = v
		return m
	}

	if got := run(updatable, base, with("tags", "b")); len(got) != 1 || got[0] != "Change to tags will not be applied" {
		t.Errorf("createOnly change: want one warning on tags, got %v", got)
	}
	if got := run(updatable, base, with("description", "d2")); len(got) != 0 {
		t.Errorf("a real update must not warn, got %v", got)
	}
	if got := run(updatable, base, base); len(got) != 0 {
		t.Errorf("no change must not warn, got %v", got)
	}
	if got := run(updatable, nil, base); len(got) != 0 {
		t.Errorf("create must not warn, got %v", got)
	}
	if got := run(immutable, base, with("description", "d2")); len(got) != 1 || got[0] != "Change to description will not be applied" {
		t.Errorf("immutable resource: any non-forceNew change warns, got %v", got)
	}
}
