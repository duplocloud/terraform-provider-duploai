package duplocloud

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// duploai_resource_group reaches the platform's metaData map twice: once as a
// free-form metadata map (apiPath "metaData") and once as the typed
// delete_protection flag (apiPath "metaData.delete_protection"). setPath writes
// the whole object for the former and a single key for the latter, so the two
// only survive together if the map is written FIRST and the leaf then merges into
// it. Spec order therefore carries meaning here, and these tests pin it down —
// swapping the two attributes in resource_group.json would silently drop
// delete_protection from the request.
func rgSpecWithMetadata() ResourceSpec {
	return ResourceSpec{
		Name: "rg", IDPath: "id",
		Endpoint: EndpointSpec{UriBase: "/rgs"},
		Attributes: []AttributeSpec{
			{Name: "name", Type: "string", Required: true, APIPath: "name"},
			{Name: "metadata", Type: "map(string)", Optional: true, Computed: true,
				APIPath: "metaData", FilterResponseKeys: []string{"delete_protection"}},
			{Name: "delete_protection", Type: "bool", Optional: true, Computed: true,
				StringBool: true, APIPath: "metaData.delete_protection"},
		},
	}
}

var rgObjType = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
	"id":                tftypes.String,
	"name":              tftypes.String,
	"metadata":          tftypes.Map{ElementType: tftypes.String},
	"delete_protection": tftypes.Bool,
}}

func rgBody(t *testing.T, r *dynamicResource, metadata, protect tftypes.Value) map[string]any {
	t.Helper()
	raw := tftypes.NewValue(rgObjType, map[string]tftypes.Value{
		"id":                tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"name":              tftypes.NewValue(tftypes.String, "rg-1"),
		"metadata":          metadata,
		"delete_protection": protect,
	})
	return r.bodyFromRaw(raw, "create", nil)
}

func TestMetadataMap_MergesWithTypedDeleteProtection(t *testing.T) {
	spec := rgSpecWithMetadata()
	if err := spec.validate(); err != nil {
		t.Fatalf("spec rejected: %v", err)
	}
	r := &dynamicResource{spec: spec}
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)

	userMeta := tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{
		"owner": tftypes.NewValue(tftypes.String, "platform-team"),
		"tier":  tftypes.NewValue(tftypes.String, "nonprod"),
	})

	// Both set: one metaData object carrying the user's keys AND the flag.
	body := rgBody(t, r, userMeta, tftypes.NewValue(tftypes.Bool, false))
	meta, ok := body["metaData"].(map[string]any)
	if !ok {
		t.Fatalf("metaData not built as an object: %#v", body["metaData"])
	}
	if got := meta["owner"]; got != "platform-team" {
		t.Errorf("metaData[owner] = %#v, want \"platform-team\"", got)
	}
	if got := meta["tier"]; got != "nonprod" {
		t.Errorf("metaData[tier] = %#v, want \"nonprod\"", got)
	}
	if got := meta["delete_protection"]; got != "false" {
		t.Errorf("metaData[delete_protection] = %#v (%T), want string \"false\"", got, got)
	}

	// Metadata only: the flag stays absent so the platform applies its default.
	body = rgBody(t, r, userMeta, tftypes.NewValue(tftypes.Bool, nil))
	meta, _ = body["metaData"].(map[string]any)
	if _, present := meta["delete_protection"]; present {
		t.Errorf("unset delete_protection was sent: %#v", meta["delete_protection"])
	}
	if got := meta["owner"]; got != "platform-team" {
		t.Errorf("metaData[owner] = %#v, want \"platform-team\"", got)
	}

	// Flag only: still produces the object, exactly as before this attribute existed.
	body = rgBody(t, r, tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil),
		tftypes.NewValue(tftypes.Bool, true))
	meta, ok = body["metaData"].(map[string]any)
	if !ok {
		t.Fatalf("metaData not built as an object: %#v", body["metaData"])
	}
	if got := meta["delete_protection"]; got != "true" {
		t.Errorf("metaData[delete_protection] = %#v, want string \"true\"", got)
	}
	if len(meta) != 1 {
		t.Errorf("metaData carried extra keys: %#v", meta)
	}

	// Neither set: nothing at all, so no empty object overwrites server state.
	body = rgBody(t, r, tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil),
		tftypes.NewValue(tftypes.Bool, nil))
	if _, present := body["metaData"]; present {
		t.Errorf("metaData sent while both attributes unset: %#v", body["metaData"])
	}
}

// A user's key must never win over the typed attribute: delete_protection is
// written after the map, so the flag's value is the one that reaches the API.
func TestMetadataMap_TypedFlagOverridesUserKey(t *testing.T) {
	r := &dynamicResource{spec: rgSpecWithMetadata()}
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)

	body := rgBody(t, r,
		tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{
			"delete_protection": tftypes.NewValue(tftypes.String, "true"),
		}),
		tftypes.NewValue(tftypes.Bool, false))

	meta, _ := body["metaData"].(map[string]any)
	if got := meta["delete_protection"]; got != "false" {
		t.Errorf("delete_protection = %#v, want the typed attribute's \"false\"", got)
	}
}

// The read side must hide delete_protection from the map, or a config that does
// not list that key shows perpetual drift as Terraform tries to remove it.
func TestMetadataMap_ReadFiltersDeleteProtection(t *testing.T) {
	a := AttributeSpec{Name: "metadata", Type: "map(string)", Optional: true, Computed: true,
		APIPath: "metaData", FilterResponseKeys: []string{"delete_protection"}}

	mapType := tftypes.Map{ElementType: tftypes.String}
	got := attrFromResponse(a, mapType, map[string]any{
		"owner":             "platform-team",
		"delete_protection": "true",
	})

	var out map[string]tftypes.Value
	if err := got.As(&out); err != nil {
		t.Fatalf("As() failed: %v", err)
	}
	if _, present := out["delete_protection"]; present {
		t.Error("delete_protection leaked into the metadata map on read")
	}
	if _, present := out["owner"]; !present {
		t.Error("user key owner was dropped from the metadata map")
	}
}
