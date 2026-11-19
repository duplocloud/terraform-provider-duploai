package duplocloud

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	// Unknown config (a value from another resource): not decided, never a spurious replacement of "".
	if _, replace := planKMS(t, types.StringValue(""), types.StringUnknown()); replace {
		t.Error("an unknown config over an empty state must not replace")
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
		{Name: "x", Type: "string", Optional: true, Computed: true, EmptyMeansUnset: true},                 // not forceNew
		{Name: "x", Type: "string", Optional: true, ForceNew: true, EmptyMeansUnset: true},                 // not computed
		{Name: "x", Type: "string", Required: true, ForceNew: true, EmptyMeansUnset: true},                 // required
		{Name: "x", Type: "string", Computed: true, ForceNew: true, EmptyMeansUnset: true},                 // computed only
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
		spec := mustLoadSpec(t, name)
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
