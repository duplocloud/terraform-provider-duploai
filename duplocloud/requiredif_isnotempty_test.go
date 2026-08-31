package duplocloud

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var awsDefault = jsonRaw(`"Aws"`)

// listVal builds a known list(string) tftypes value from the given elements.
func listVal(elems ...string) tftypes.Value {
	vs := make([]tftypes.Value, 0, len(elems))
	for _, e := range elems {
		vs = append(vs, tftypes.NewValue(tftypes.String, e))
	}
	return tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, vs)
}

// isNotEmpty is the operator that makes an all-or-nothing pair expressible: two
// requiredIf rules pointing at each other, so setting either list makes the other
// mandatory. This is the shape network_baseline needs for its custom subnet CIDRs,
// where the API rejects one list without the other.
//
// The list cases also pin the collection-aware emptiness rule: an EXPLICIT empty
// list counts as not set, because the API tests the list's length. Without
// attrEmptyAtPath, readConfigString renders []string{} as "[]" — a non-empty
// string — so isNotEmpty would wrongly hold for an empty list.
func TestRequiredIf_IsNotEmpty_AllOrNothingPair(t *testing.T) {
	spec := ResourceSpec{
		Name: "net", IDPath: "id",
		Endpoint: EndpointSpec{UriBase: "/nets"},
		RequiredIf: []RequiredIfRule{
			{Attribute: "private_cidrs", When: []RequiredIfCondition{{Attribute: "public_cidrs", IsNotEmpty: true}}},
			{Attribute: "public_cidrs", When: []RequiredIfCondition{{Attribute: "private_cidrs", IsNotEmpty: true}}},
		},
		Attributes: []AttributeSpec{
			{Name: "public_cidrs", Type: "list(string)", Optional: true, Computed: true, APIPath: "spec.customPublicSubnetCidrs"},
			{Name: "private_cidrs", Type: "list(string)", Optional: true, Computed: true, APIPath: "spec.customPrivateSubnetCidrs"},
		},
	}
	if err := spec.validate(); err != nil {
		t.Fatalf("spec with isNotEmpty conditions rejected: %v", err)
	}

	r := &dynamicResource{spec: spec}
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	v := requiredIfValidator{spec: spec}

	strList := tftypes.List{ElementType: tftypes.String}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":            tftypes.String,
		"public_cidrs":  strList,
		"private_cidrs": strList,
	}}
	run := func(public, private tftypes.Value) bool {
		raw := tftypes.NewValue(objType, map[string]tftypes.Value{
			"id":            tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			"public_cidrs":  public,
			"private_cidrs": private,
		})
		req := resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: raw}}
		resp := resource.ValidateConfigResponse{}
		v.ValidateResource(context.Background(), req, &resp)
		return resp.Diagnostics.HasError()
	}

	nullList := tftypes.NewValue(strList, nil)
	unknownList := tftypes.NewValue(strList, tftypes.UnknownValue)

	if run(nullList, nullList) {
		t.Error("requiredIf fired with neither list set — all-or-nothing allows neither")
	}
	if run(listVal("10.0.0.0/24"), listVal("10.0.1.0/24")) {
		t.Error("requiredIf fired with both lists set")
	}
	if !run(listVal("10.0.0.0/24"), nullList) {
		t.Error("requiredIf did not fire when only public_cidrs was set")
	}
	if !run(nullList, listVal("10.0.1.0/24")) {
		t.Error("requiredIf did not fire when only private_cidrs was set")
	}
	if run(listVal(), nullList) {
		t.Error("requiredIf fired for an explicitly EMPTY public_cidrs — an empty list is not set")
	}
	if run(unknownList, nullList) {
		t.Error("requiredIf fired on an unknown (referenced) public_cidrs — unknown must be treated as set")
	}
}

// isNotEmpty on a scalar keeps the plain string semantics: set means non-empty
// after the attribute's default is applied.
func TestRequiredIf_IsNotEmpty_Scalar(t *testing.T) {
	spec := ResourceSpec{
		Name: "thing", IDPath: "id",
		Endpoint: EndpointSpec{UriBase: "/things"},
		RequiredIf: []RequiredIfRule{
			{Attribute: "kms_key_id", When: []RequiredIfCondition{{Attribute: "snapshot_name", IsNotEmpty: true}}},
		},
		Attributes: []AttributeSpec{
			{Name: "snapshot_name", Type: "string", Optional: true, APIPath: "spec.snapshotName"},
			{Name: "kms_key_id", Type: "string", Optional: true, APIPath: "spec.kmsKeyId"},
		},
	}
	r := &dynamicResource{spec: spec}
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	v := requiredIfValidator{spec: spec}

	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":            tftypes.String,
		"snapshot_name": tftypes.String,
		"kms_key_id":    tftypes.String,
	}}
	run := func(snapshot tftypes.Value) bool {
		raw := tftypes.NewValue(objType, map[string]tftypes.Value{
			"id":            tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			"snapshot_name": snapshot,
			"kms_key_id":    tftypes.NewValue(tftypes.String, nil),
		})
		req := resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: raw}}
		resp := resource.ValidateConfigResponse{}
		v.ValidateResource(context.Background(), req, &resp)
		return resp.Diagnostics.HasError()
	}

	if !run(tftypes.NewValue(tftypes.String, "snap-1")) {
		t.Error("requiredIf did not fire when snapshot_name was set")
	}
	if run(tftypes.NewValue(tftypes.String, nil)) {
		t.Error("requiredIf fired when snapshot_name was null")
	}
	if run(tftypes.NewValue(tftypes.String, "")) {
		t.Error("requiredIf fired for an empty-string snapshot_name — empty is not set")
	}
}

// invalidWhen shares the condition vocabulary, so isNotEmpty must work there too —
// network_baseline uses it to reject custom subnet CIDRs on a non-AWS cloud and in
// Import mode. Before attrEmptyAtPath, readPathString rendered every collection as
// "" regardless of content, so a list could never be seen as set from invalidWhen.
func TestInvalidWhen_IsNotEmptyOnList(t *testing.T) {
	spec := ResourceSpec{
		Name: "net", IDPath: "id",
		Endpoint: EndpointSpec{UriBase: "/nets"},
		InvalidWhen: []InvalidWhenRule{
			{
				Attribute: "public_cidrs",
				When: []RequiredIfCondition{
					{Attribute: "cloud", NotEquals: "Aws"},
					{Attribute: "public_cidrs", IsNotEmpty: true},
				},
				Message: "public_cidrs is AWS only.",
			},
		},
		Attributes: []AttributeSpec{
			{Name: "cloud", Type: "string", Optional: true, Computed: true, Default: &awsDefault, APIPath: "spec.cloud"},
			{Name: "public_cidrs", Type: "list(string)", Optional: true, Computed: true, APIPath: "spec.customPublicSubnetCidrs"},
		},
	}
	if err := spec.validate(); err != nil {
		t.Fatalf("invalidWhen spec with isNotEmpty rejected: %v", err)
	}

	r := &dynamicResource{spec: spec}
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)

	strList := tftypes.List{ElementType: tftypes.String}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":           tftypes.String,
		"cloud":        tftypes.String,
		"public_cidrs": strList,
	}}
	run := func(cloud string, public tftypes.Value) bool {
		raw := tftypes.NewValue(objType, map[string]tftypes.Value{
			"id":           tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			"cloud":        tftypes.NewValue(tftypes.String, cloud),
			"public_cidrs": public,
		})
		req := resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: raw}}
		resp := resource.ValidateConfigResponse{}
		invalidWhenValidator{spec: spec}.ValidateResource(context.Background(), req, &resp)
		return resp.Diagnostics.HasError()
	}

	if !run("Azure", listVal("10.0.0.0/24")) {
		t.Error("invalidWhen did not fire for custom CIDRs on Azure")
	}
	if run("Aws", listVal("10.0.0.0/24")) {
		t.Error("invalidWhen fired for custom CIDRs on Aws")
	}
	if run("Azure", tftypes.NewValue(strList, nil)) {
		t.Error("invalidWhen fired on Azure with no custom CIDRs set")
	}
	if run("Azure", listVal()) {
		t.Error("invalidWhen fired on Azure with an explicitly empty list — an empty list is not set")
	}
}

// The spec loader must reject a condition that sets isNotEmpty alongside another
// operator, for requiredIf and invalidWhen alike — a silently-ignored operator
// would turn a validation rule into a no-op.
func TestSpecValidate_IsNotEmptyOperatorExclusivity(t *testing.T) {
	base := func(s ResourceSpec) error {
		s.Name, s.IDPath = "x", "id"
		s.Endpoint = EndpointSpec{UriBase: "/x"}
		s.Attributes = append(s.Attributes,
			AttributeSpec{Name: "a", Type: "list(string)", Optional: true},
			AttributeSpec{Name: "b", Type: "list(string)", Optional: true},
		)
		return s.validate()
	}
	if err := base(ResourceSpec{RequiredIf: []RequiredIfRule{
		{Attribute: "a", When: []RequiredIfCondition{{Attribute: "b", IsNotEmpty: true, IsEmpty: true}}},
	}}); err == nil {
		t.Error("expected error when a requiredIf condition sets both isNotEmpty and isEmpty")
	}
	if err := base(ResourceSpec{InvalidWhen: []InvalidWhenRule{
		{Attribute: "a", Message: "m", When: []RequiredIfCondition{{Attribute: "b", IsNotEmpty: true, Equals: "x"}}},
	}}); err == nil {
		t.Error("expected error when an invalidWhen condition sets both isNotEmpty and equals")
	}
}
