package duplocloud

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// A requiredIf target that is set to an UNKNOWN value — e.g. a reference to
// another resource created in the same apply (network_id = network.x.network_id)
// — must NOT trigger the "missing required attribute" error. Only a genuinely
// null (omitted) value should. Regression for configAttrNull treating unknown
// as null.
func TestRequiredIf_UnknownTargetNotFlagged(t *testing.T) {
	spec := ResourceSpec{
		Name: "thing", IDPath: "id",
		Endpoint: EndpointSpec{UriBase: "/things"},
		RequiredIf: []RequiredIfRule{
			{Attribute: "network_id", When: []RequiredIfCondition{{Attribute: "cloud", NotEquals: "K8S_ONLY"}}},
		},
		Attributes: []AttributeSpec{
			{Name: "cloud", Type: "string", Optional: true, Computed: true, APIPath: "spec.cloud"},
			{Name: "network_id", Type: "string", Optional: true, APIPath: "spec.networkId"},
		},
	}
	r := &dynamicResource{spec: spec}
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	v := requiredIfValidator{spec: spec}

	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":         tftypes.String,
		"cloud":      tftypes.String,
		"network_id": tftypes.String,
	}}
	run := func(networkID tftypes.Value) bool {
		raw := tftypes.NewValue(objType, map[string]tftypes.Value{
			"id":         tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			"cloud":      tftypes.NewValue(tftypes.String, "Aws"),
			"network_id": networkID,
		})
		req := resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: raw}}
		resp := resource.ValidateConfigResponse{}
		v.ValidateResource(context.Background(), req, &resp)
		return resp.Diagnostics.HasError()
	}

	if run(tftypes.NewValue(tftypes.String, tftypes.UnknownValue)) {
		t.Error("requiredIf fired on an unknown (referenced) network_id — should treat unknown as set")
	}
	if !run(tftypes.NewValue(tftypes.String, nil)) {
		t.Error("requiredIf did not fire on a genuinely null network_id when the condition held")
	}
	if run(tftypes.NewValue(tftypes.String, "net-123")) {
		t.Error("requiredIf fired on a set network_id")
	}
}

// A requiredIf rule's CONDITION attribute (the "when" clause) being UNKNOWN —
// e.g. region required "when network_baseline_id isEmpty", and
// network_baseline_id = duploai_network_baseline.x.network_id (unresolved in
// the same apply) — must NOT be treated as empty. readConfigString collapses
// unknown to "" the same as null, so without readConfigUnknown the isEmpty
// condition would wrongly hold and demand region even though
// network_baseline_id is plainly set. Regression for the duploai_plan
// first-apply case (network_baseline + plan created together).
func TestRequiredIf_UnknownConditionAttrDoesNotFire(t *testing.T) {
	spec := ResourceSpec{
		Name: "plan", IDPath: "id",
		Endpoint: EndpointSpec{UriBase: "/plans"},
		RequiredIf: []RequiredIfRule{
			{Attribute: "region", When: []RequiredIfCondition{{Attribute: "network_baseline_id", IsEmpty: true}}},
		},
		Attributes: []AttributeSpec{
			{Name: "region", Type: "string", Optional: true, Computed: true, APIPath: "spec.region"},
			{Name: "network_baseline_id", Type: "string", Optional: true, Computed: true, APIPath: "spec.networkBaselineId"},
		},
	}
	r := &dynamicResource{spec: spec}
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	v := requiredIfValidator{spec: spec}

	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":                  tftypes.String,
		"region":              tftypes.String,
		"network_baseline_id": tftypes.String,
	}}
	run := func(region, networkBaselineID tftypes.Value) bool {
		raw := tftypes.NewValue(objType, map[string]tftypes.Value{
			"id":                  tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			"region":              region,
			"network_baseline_id": networkBaselineID,
		})
		req := resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: raw}}
		resp := resource.ValidateConfigResponse{}
		v.ValidateResource(context.Background(), req, &resp)
		return resp.Diagnostics.HasError()
	}

	if run(tftypes.NewValue(tftypes.String, nil), tftypes.NewValue(tftypes.String, tftypes.UnknownValue)) {
		t.Error("requiredIf fired on region when network_baseline_id is an unresolved reference — should not treat unknown as empty")
	}
	if !run(tftypes.NewValue(tftypes.String, nil), tftypes.NewValue(tftypes.String, nil)) {
		t.Error("requiredIf did not fire on region when network_baseline_id is genuinely null")
	}
	if run(tftypes.NewValue(tftypes.String, "us-east-1"), tftypes.NewValue(tftypes.String, nil)) {
		t.Error("requiredIf fired on region when region is set and network_baseline_id is null")
	}
}
