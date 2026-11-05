package duplocloud

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// DUPLOAI-4739 follow-up: after `terraform import`, state holds the
// server-generated externalId in credential_fields (there is no base to align
// against), while config lists only iamRoleArn. The first plan is one in-place
// update (the backend keeps the stored externalId); ModifyPlan must leave that
// plan untouched, because pinning it to state would also hide a key the user
// really removed (see TestAdminProviderImport_RemovedConfiguredKeyIsUpdate).

var impTopType = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
	"id":          tftypes.String,
	"name":        tftypes.String,
	"credentials": extIDCredListType,
	"created_at":  tftypes.String,
	"updated_at":  tftypes.String,
	"version":     tftypes.Number,
}}

func impResource() *dynamicResource {
	attrs := append(extIDAttrs(),
		AttributeSpec{Name: "created_at", Type: "string", Computed: true, APIPath: "createdAt"},
		AttributeSpec{Name: "updated_at", Type: "string", Computed: true, APIPath: "updatedAt"},
		AttributeSpec{Name: "version", Type: "int", Computed: true, APIPath: "version"},
	)
	return &dynamicResource{spec: ResourceSpec{Name: "admin_provider", IDPath: "id", Attributes: attrs}}
}

func impTop(fields []tftypes.Value, computedKnown bool) tftypes.Value {
	var top map[string]tftypes.Value
	_ = extIDTop(fields, !computedKnown).As(&top)
	top["id"] = tftypes.NewValue(tftypes.String, "p1")
	if computedKnown {
		top["created_at"] = tftypes.NewValue(tftypes.String, "2026-11-01T10:00:00Z")
		top["updated_at"] = tftypes.NewValue(tftypes.String, "2026-11-01T11:00:00Z")
		top["version"] = tftypes.NewValue(tftypes.Number, 3)
	} else {
		top["created_at"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
		top["updated_at"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
		top["version"] = tftypes.NewValue(tftypes.Number, tftypes.UnknownValue)
	}
	return tftypes.NewValue(impTopType, top)
}

// impConfig is the config: configured keys/values only, every computed leaf null.
func impConfig(kv ...string) tftypes.Value {
	var fields []tftypes.Value
	for i := 0; i < len(kv); i += 2 {
		fields = append(fields, tftypes.NewValue(extIDFieldType, map[string]tftypes.Value{
			"key":          tftypes.NewValue(tftypes.String, kv[i]),
			"value":        tftypes.NewValue(tftypes.String, kv[i+1]),
			"type":         tftypes.NewValue(tftypes.String, nil),
			"is_sensitive": tftypes.NewValue(tftypes.Bool, nil),
		}))
	}
	var top map[string]tftypes.Value
	_ = extIDTop(fields, false).As(&top)
	top["id"] = tftypes.NewValue(tftypes.String, nil)
	top["created_at"] = tftypes.NewValue(tftypes.String, nil)
	top["updated_at"] = tftypes.NewValue(tftypes.String, nil)
	top["version"] = tftypes.NewValue(tftypes.Number, nil)
	return tftypes.NewValue(impTopType, top)
}

// impPlanFields mimics the framework plan for config kv: unset
// optional+computed leaves take the prior state value at the same index
// (UseStateForUnknown), or stay unknown past the end of state.
func impPlanFields(state []tftypes.Value, kv ...string) []tftypes.Value {
	var out []tftypes.Value
	for i := 0; i < len(kv); i += 2 {
		f := extIDPlanField(kv[i], kv[i+1])
		if j := i / 2; j < len(state) {
			var fm, sm map[string]tftypes.Value
			_ = f.As(&fm)
			_ = state[j].As(&sm)
			fm["type"], fm["is_sensitive"] = sm["type"], sm["is_sensitive"]
			f = tftypes.NewValue(extIDFieldType, fm)
		}
		out = append(out, f)
	}
	return out
}

// impPlanFieldsByKey is impPlanFields but copies unset leaves from the state
// element with the same key, so a removal does not also flip type/is_sensitive
// (which would make the bodies differ for an unrelated reason).
func impPlanFieldsByKey(state []tftypes.Value, kv ...string) []tftypes.Value {
	var out []tftypes.Value
	for i := 0; i < len(kv); i += 2 {
		f := extIDPlanField(kv[i], kv[i+1])
		for _, se := range state {
			var fm, sm map[string]tftypes.Value
			_ = se.As(&sm)
			var k string
			if sm["key"].As(&k) != nil || k != kv[i] {
				continue
			}
			_ = f.As(&fm)
			fm["type"], fm["is_sensitive"] = sm["type"], sm["is_sensitive"]
			f = tftypes.NewValue(extIDFieldType, fm)
		}
		out = append(out, f)
	}
	return out
}

func impModifyPlan(t *testing.T, state, plan, config tftypes.Value) tftypes.Value {
	t.Helper()
	resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Raw: plan}}
	impResource().ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		State: tfsdk.State{Raw: state}, Plan: tfsdk.Plan{Raw: plan}, Config: tfsdk.Config{Raw: config},
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("diags: %v", resp.Diagnostics)
	}
	return resp.Plan.Raw
}

func impImportedFields(externalIDFirst bool) []tftypes.Value {
	role := extIDStateField("iamRoleArn", extIDRoleArn, true)
	ext := extIDStateField("externalId", extIDGenerated, false)
	if externalIDFirst {
		return []tftypes.Value{ext, role}
	}
	return []tftypes.Value{role, ext}
}

func impImportedState() tftypes.Value { return impTop(impImportedFields(false), true) }

func TestAdminProviderImport_StateOnlyKeyedElementPlansInPlaceUpdate(t *testing.T) {
	for name, extFirst := range map[string]bool{"externalId last": false, "externalId first": true} {
		t.Run(name, func(t *testing.T) {
			fields := impImportedFields(extFirst)
			state := impTop(fields, true)
			for pname, mkPlan := range map[string]func() tftypes.Value{
				"leaves from state by index": func() tftypes.Value { return impTop(impPlanFields(fields, "iamRoleArn", extIDRoleArn), false) },
				"leaves unknown": func() tftypes.Value {
					return impTop([]tftypes.Value{extIDPlanField("iamRoleArn", extIDRoleArn)}, false)
				},
			} {
				t.Run(pname, func(t *testing.T) {
					got := impModifyPlan(t, state, mkPlan(), impConfig("iamRoleArn", extIDRoleArn))
					// One in-place update: the plan is exactly what Terraform built.
					if got.Equal(state) {
						t.Error("import plan pinned to state; want one in-place update")
					}
					if !got.Equal(mkPlan()) {
						t.Error("import plan altered by ModifyPlan; want it untouched")
					}
					if !state.Equal(impTop(impImportedFields(extFirst), true)) {
						t.Error("ModifyPlan mutated the prior state value")
					}
				})
			}
		})
	}
}

func TestAdminProviderImport_RealChangeStillDiffs(t *testing.T) {
	state := impImportedState()
	cases := map[string][]tftypes.Value{
		"edited value": {extIDPlanField("iamRoleArn", "arn:aws:iam::123456789012:role/other")},
		"added key": {
			extIDPlanField("iamRoleArn", extIDRoleArn), extIDPlanField("region", "us-west-2"),
		},
		"configured key removed": {extIDPlanField("externalId", extIDGenerated)},
		"externalId edited": {
			extIDPlanField("iamRoleArn", extIDRoleArn), extIDPlanField("externalId", "other"),
		},
	}
	for name, fields := range cases {
		t.Run(name, func(t *testing.T) {
			var kv []string
			for _, f := range fields {
				var fm map[string]tftypes.Value
				_ = f.As(&fm)
				var k, v string
				_ = fm["key"].As(&k)
				_ = fm["value"].As(&v)
				kv = append(kv, k, v)
			}
			plan := impTop(impPlanFieldsByKey(impImportedFields(false), kv...), false)
			got := impModifyPlan(t, state, plan, impConfig(kv...))
			// Compare with a freshly built plan: ModifyPlan must not mutate req.Plan.
			if !got.Equal(impTop(impPlanFieldsByKey(impImportedFields(false), kv...), false)) {
				t.Errorf("real change: ModifyPlan altered the plan; want it untouched (computed stay unknown, credentials as configured)")
			}
		})
	}
}

// Removing a configured key (region) after import is a real update: the plan
// must stay as Terraform built it, not be pinned back to state ("No changes").
func TestAdminProviderImport_RemovedConfiguredKeyIsUpdate(t *testing.T) {
	fields := []tftypes.Value{
		extIDStateField("iamRoleArn", extIDRoleArn, true),
		extIDStateField("region", "us-west-2", false),
		extIDStateField("externalId", extIDGenerated, false),
	}
	state := impTop(fields, true)
	for pname, mkPlan := range map[string]func() tftypes.Value{
		"leaves from state by index": func() tftypes.Value { return impTop(impPlanFields(fields, "iamRoleArn", extIDRoleArn), false) },
		"leaves unknown": func() tftypes.Value {
			return impTop([]tftypes.Value{extIDPlanField("iamRoleArn", extIDRoleArn)}, false)
		},
	} {
		t.Run(pname, func(t *testing.T) {
			got := impModifyPlan(t, state, mkPlan(), impConfig("iamRoleArn", extIDRoleArn))
			if got.Equal(state) {
				t.Fatal("removed key region: ModifyPlan pinned plan to state (No changes); want a real update")
			}
			if !got.Equal(mkPlan()) {
				t.Error("removed key region: ModifyPlan altered the plan; want it untouched")
			}
		})
	}
}

// created_at is assigned once, so a real update must not show it as
// "(known after apply)". updated_at/version legitimately change on every
// update and must stay recomputed (stable would cause inconsistent results).
func TestAdminProviderSpec_CreatedAtStableOnly(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range specs {
		if s.Name != "admin_provider" {
			continue
		}
		want := map[string]bool{"created_at": true, "updated_at": false, "version": false}
		for _, a := range s.Attributes {
			if w, ok := want[a.Name]; ok && a.Stable != w {
				t.Errorf("%s stable = %v, want %v", a.Name, a.Stable, w)
			}
		}
		return
	}
	t.Fatal("admin_provider spec not found")
}
