package duplocloud

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// kms_key_id on the RDS resources: an explicit "" means "not specified". It must
// never replace the database, never be sent, never plan an unknown (an unknown
// would replace everything that references the attribute: the storage_account
// RCA), and must not drift when the platform reports the key it chose.

func kmsAttr() AttributeSpec {
	return AttributeSpec{Name: "kms_key_id", Type: "string", Optional: true, Computed: true, ForceNew: true,
		EmptyMeansUnset: true, APIPath: "spec.storageKmsKeyId"}
}

// planKMS runs the attribute's plan modifiers like the framework does and
// returns the resulting plan value and whether a replacement was requested.
func planKMS(t *testing.T, state, config types.String) (types.String, bool) {
	t.Helper()
	obj := tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})
	// A null config on an optional+computed attribute is planned unknown by the framework.
	plan := config
	if config.IsNull() {
		plan = types.StringUnknown()
	}
	req := planmodifier.StringRequest{
		State: tfsdk.State{Raw: obj}, Plan: tfsdk.Plan{Raw: obj},
		StateValue: state, PlanValue: plan, ConfigValue: config,
	}
	resp := &planmodifier.StringResponse{PlanValue: plan}
	for _, m := range attrSchema(kmsAttr()).(schema.StringAttribute).PlanModifiers {
		req.PlanValue = resp.PlanValue
		m.PlanModifyString(context.Background(), req, resp)
	}
	return resp.PlanValue, resp.RequiresReplace
}

func TestEmptyMeansUnset_Plan(t *testing.T) {
	key := types.StringValue("arn:aws:kms:us-west-2:111:key/abc")
	cases := []struct {
		name          string
		state, config types.String
		wantReplace   bool
		wantPlan      string // "" with wantKnown false means unknown is not expected either
	}{
		{"empty over null state (import, or created unset)", types.StringNull(), types.StringValue(""), false, ""},
		{"empty over the platform's default key", key, types.StringValue(""), false, ""},
		{"empty over empty", types.StringValue(""), types.StringValue(""), false, ""},
		{"a real key replaces an empty one", types.StringValue(""), key, true, key.ValueString()},
		{"a different key still replaces", key, types.StringValue("arn:aws:kms:us-west-2:111:key/other"), true, "arn:aws:kms:us-west-2:111:key/other"},
		{"unset keeps the stored key, no replacement", key, types.StringNull(), false, key.ValueString()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan, replace := planKMS(t, c.state, c.config)
			if replace != c.wantReplace {
				t.Errorf("replace = %v, want %v", replace, c.wantReplace)
			}
			if plan.IsUnknown() {
				t.Fatalf("plan must never be unknown here (would replace dependents), got unknown")
			}
			if plan.ValueString() != c.wantPlan {
				t.Errorf("plan = %q, want %q", plan.ValueString(), c.wantPlan)
			}
		})
	}
	// Unknown config (a key from another resource) over an empty state is not decided:
	// it is plain forceNew. Skipping the replacement would let a real key arriving at
	// apply change an immutable field with no replacement, silently discarded.
	if plan, replace := planKMS(t, types.StringValue(""), types.StringUnknown()); !replace || !plan.IsUnknown() {
		t.Errorf("an unknown config over an empty state keeps plain forceNew behaviour, got replace=%v plan=%v", replace, plan)
	}
}

func TestEmptyMeansUnset_BodyOmitsEmpty(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"id": tftypes.String, "kms_key_id": tftypes.String}}
	r := &dynamicResource{spec: ResourceSpec{Attributes: []AttributeSpec{kmsAttr()}}}
	body := func(v tftypes.Value, verb string) map[string]any {
		var diags diag.Diagnostics
		raw := tftypes.NewValue(objType, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "x"), "kms_key_id": v})
		out := r.bodyFromRaw(raw, verb, &diags)
		if diags.HasError() {
			t.Fatalf("diags: %v", diags)
		}
		return out
	}
	for _, verb := range []string{"create", "update"} {
		if b := body(str(""), verb); len(b) != 0 {
			t.Errorf("%s: an empty kms_key_id must not be sent, got %v", verb, b)
		}
		b := body(str("arn:key"), verb)
		spec, _ := b["spec"].(map[string]any)
		if spec["storageKmsKeyId"] != "arn:key" {
			t.Errorf("%s: a real key must still be sent, got %v", verb, b)
		}
	}
}

func TestEmptyMeansUnset_KeepOnRead(t *testing.T) {
	a := kmsAttr()
	key := str("arn:aws:kms:us-west-2:111:key/abc")
	if got := keepEmptyUnset(a, str(""), key); !got.Equal(str("")) {
		t.Errorf("a held empty value must survive the platform reporting its key, got %v", got)
	}
	if got := keepEmptyUnset(a, tftypes.NewValue(tftypes.String, nil), key); !got.Equal(key) {
		t.Errorf("a null held value takes what the API reports, got %v", got)
	}
	if got := keepEmptyUnset(a, str("arn:other"), key); !got.Equal(key) {
		t.Errorf("a real held value is not touched here, got %v", got)
	}
	if got := keepEmptyUnset(a, tftypes.NewValue(tftypes.String, tftypes.UnknownValue), key); !got.Equal(key) {
		t.Errorf("an unknown held value is not touched, got %v", got)
	}
	plain := AttributeSpec{Type: "string", Optional: true, Computed: true, ForceNew: true}
	if got := keepEmptyUnset(plain, str(""), key); !got.Equal(key) {
		t.Errorf("without the flag the read is unchanged, got %v", got)
	}
}

func TestEmptyMeansUnset_Validation(t *testing.T) {
	bad := []AttributeSpec{
		{Name: "x", Type: "int", Optional: true, Computed: true, ForceNew: true, EmptyMeansUnset: true},
		{Name: "x", Type: "string", Optional: true, Computed: true, EmptyMeansUnset: true}, // not forceNew
		{Name: "x", Type: "string", Optional: true, ForceNew: true, EmptyMeansUnset: true}, // not computed
		{Name: "x", Type: "string", Required: true, ForceNew: true, EmptyMeansUnset: true}, // required
		{Name: "x", Type: "string", Computed: true, ForceNew: true, EmptyMeansUnset: true}, // computed only
	}
	for i, a := range bad {
		if _, err := validateAttributes([]AttributeSpec{a}); err == nil || !strings.Contains(err.Error(), "emptyMeansUnset") {
			t.Errorf("case %d: expected an emptyMeansUnset validation error, got %v", i, err)
		}
	}
	if _, err := validateAttributes([]AttributeSpec{kmsAttr()}); err != nil {
		t.Errorf("a valid spec must load, got %v", err)
	}
}

// The shipped RDS specs carry the flag, so the fix cannot be silently dropped.
func TestEmptyMeansUnset_RDSSpecsOptIn(t *testing.T) {
	for _, name := range []string{"rds_cluster", "rds_instance"} {
		spec := customerManagedSpecs(t)[name]
		if spec == nil {
			t.Fatalf("no embedded spec %q", name)
		}
		found := false
		for _, a := range spec.Attributes {
			if a.Name == "kms_key_id" {
				found = true
				if !a.EmptyMeansUnset {
					t.Errorf("%s.kms_key_id must set emptyMeansUnset", name)
				}
			}
		}
		if !found {
			t.Errorf("%s has no kms_key_id attribute", name)
		}
	}
}

// ModifyPlan end to end: omitting "" from the plan body makes the plan body equal
// the state body, and ModifyPlan then pins every computed attribute to its prior
// value. A configured "" must survive that (a plan different from a non-null
// config is rejected by Terraform), and a stored key against "" must be a no-op,
// not an update that omits the key.
func TestEmptyMeansUnset_ModifyPlanEndToEnd(t *testing.T) {
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"name": tftypes.String, "kms_key_id": tftypes.String}}
	name := AttributeSpec{Name: "name", Type: "string", Optional: true, APIPath: "spec.name"}
	r := &dynamicResource{spec: ResourceSpec{Attributes: []AttributeSpec{name, kmsAttr()}}}
	null := tftypes.NewValue(tftypes.String, nil)
	obj := func(kms tftypes.Value) tftypes.Value {
		return tftypes.NewValue(objType, map[string]tftypes.Value{"name": str("db"), "kms_key_id": kms})
	}
	plan := func(state, planned tftypes.Value) tftypes.Value {
		resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Raw: planned}}
		r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
			State: tfsdk.State{Raw: state}, Plan: tfsdk.Plan{Raw: planned}, Config: tfsdk.Config{Raw: planned},
		}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("diags: %v", resp.Diagnostics)
		}
		return resp.Plan.Raw
	}
	kmsOf := func(v tftypes.Value) tftypes.Value {
		var m map[string]tftypes.Value
		_ = v.As(&m)
		return m["kms_key_id"]
	}
	key := str("arn:aws:kms:us-west-2:111:key/abc")

	for name, state := range map[string]tftypes.Value{"null state (import / created unset)": obj(null), "stored key": obj(key), "empty state": obj(str(""))} {
		t.Run("config empty over "+name, func(t *testing.T) {
			if got := kmsOf(plan(state, obj(str("")))); !got.Equal(str("")) {
				t.Errorf("a configured \"\" must plan as \"\", got %v", got)
			}
			// No PUT: the comparison Update makes must see equal bodies.
			var d diag.Diagnostics
			body := r.bodyFromRaw(obj(str("")), "update", &d)
			sb := r.bodyFromRaw(state, "update", &d)
			r.omitEmptyUnsetFromState(obj(str("")), sb)
			if !apiBodyEqual(body, sb) {
				t.Errorf("\"\" must be a no-op against %s, plan body %v vs state body %v", name, body, sb)
			}
		})
	}
	t.Run("a different key is still a real update", func(t *testing.T) {
		var d diag.Diagnostics
		body := r.bodyFromRaw(obj(str("arn:other")), "update", &d)
		sb := r.bodyFromRaw(obj(key), "update", &d)
		r.omitEmptyUnsetFromState(obj(str("arn:other")), sb)
		if apiBodyEqual(body, sb) {
			t.Error("a changed non-empty key must not be treated as a no-op")
		}
	})
	t.Run("unset config keeps pinning the stored key", func(t *testing.T) {
		if got := kmsOf(plan(obj(key), obj(key))); !got.Equal(key) {
			t.Errorf("got %v", got)
		}
	})
}
