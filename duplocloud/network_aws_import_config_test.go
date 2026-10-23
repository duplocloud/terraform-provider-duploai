package duplocloud

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// network_baseline.aws_import_config (spec.awsImportConfig) was added after networks were already in
// production. These tests pin what keeps it backward compatible for existing configurations and state:
//
//   - optional + computed with a keep-null UseStateForUnknown, so a config without the block plans the
//     stored value (null for every pre-existing network, or whatever the UI saved) — never a diff;
//   - no default and no forceNew, so adding the attribute can never plan a replacement;
//   - a config without the block sends the same request body as before (no awsImportConfig key);
//   - none of the new plan-time rules fire for a config that does not set the block.

func TestNetworkAwsImportConfigIsBackwardCompatible(t *testing.T) {
	a := customerManagedSpecs(t)["network_baseline"].attr("aws_import_config")
	if a == nil {
		t.Fatal("network_baseline has no aws_import_config attribute")
	}
	if !a.Optional || !a.Computed || a.Required {
		t.Errorf("optional=%v computed=%v required=%v, want optional + computed", a.Optional, a.Computed, a.Required)
	}
	if a.ForceNew || a.Default != nil {
		t.Errorf("forceNew=%v default=%v, want neither: either would plan a change for existing networks", a.ForceNew, a.Default)
	}
	if a.APIPath != "spec.awsImportConfig" {
		t.Errorf("apiPath=%q, want spec.awsImportConfig", a.APIPath)
	}
	if !useStateForUnknown(*a) || preserveNonNullOnly(*a) {
		t.Errorf("useStateForUnknown=%v preserveNonNullOnly=%v, want a modifier that keeps a null from state",
			useStateForUnknown(*a), preserveNonNullOnly(*a))
	}
	mods := attrSchema(*a).(schema.SingleNestedAttribute).PlanModifiers
	if len(mods) != 1 || reflect.TypeOf(mods[0]) != reflect.TypeOf(objectplanmodifier.UseStateForUnknown()) {
		t.Errorf("plan modifiers = %v, want [UseStateForUnknown]", mods)
	}
	for _, na := range a.Attributes {
		if na.Required || na.ForceNew || na.Default != nil {
			t.Errorf("%s: required=%v forceNew=%v default=%v, want an optional leaf with no default",
				na.Name, na.Required, na.ForceNew, na.Default)
		}
	}
}

// A config without the block — every configuration written before this attribute existed — must not
// put awsImportConfig in the create or update body.
func TestNetworkAwsImportConfigOmittedFromBodyWhenUnset(t *testing.T) {
	spec := customerManagedSpecs(t)["network_baseline"]
	ctx := context.Background()
	r := &dynamicResource{spec: *spec}
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	objType := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)

	vals := map[string]tftypes.Value{}
	for name, typ := range objType.AttributeTypes {
		vals[name] = tftypes.NewValue(typ, nil)
	}
	vals["workspace_id"] = tftypes.NewValue(tftypes.String, "ws")
	vals["name"] = tftypes.NewValue(tftypes.String, "net")
	vals["mode"] = tftypes.NewValue(tftypes.String, "Import")
	vals["vpc_id"] = tftypes.NewValue(tftypes.String, "vpc-0a1b2c3d")
	raw := tftypes.NewValue(objType, vals)

	for _, verb := range []string{"create", "update"} {
		var diags diag.Diagnostics
		body := r.bodyFromRaw(raw, verb, &diags)
		if diags.HasError() {
			t.Fatalf("%s: %v", verb, diags)
		}
		spec, _ := body["spec"].(map[string]any)
		if _, present := spec["awsImportConfig"]; present {
			t.Errorf("%s body carries awsImportConfig for a config that does not set it: %v", verb, spec["awsImportConfig"])
		}
		if spec["importVpcId"] != "vpc-0a1b2c3d" {
			t.Errorf("%s body importVpcId = %v, want vpc-0a1b2c3d", verb, spec["importVpcId"])
		}
	}
}

// Configurations that predate aws_import_config must still validate.
func TestNetworkAwsImportConfigRulesIgnoreExistingConfigs(t *testing.T) {
	for _, set := range []map[string]string{
		{},
		{"mode": "Import"},
		{"mode": "Import", "vpc_id": "vpc-0a1b2c3d"},
		{"cloud": "Azure"},
		{"cloud": "Azure", "mode": "Import"},
	} {
		if invalidWhenFires(t, "network_baseline", set) {
			t.Errorf("%v: invalidWhen fired for a config without aws_import_config", set)
		}
	}
}
