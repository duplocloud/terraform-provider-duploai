package duplocloud

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// stringBool exists because the platform keeps delete_protection in a
// Dictionary<string,string> metadata map, which cannot hold a JSON boolean. The
// request must therefore carry the STRING "true"/"false" while the schema stays a
// real bool, so HCL reads `delete_protection = false` like every other flag.
func TestStringBool_RequestCarriesStringNotJSONBool(t *testing.T) {
	spec := ResourceSpec{
		Name: "rg", IDPath: "id",
		Endpoint: EndpointSpec{UriBase: "/rgs"},
		Attributes: []AttributeSpec{
			{Name: "name", Type: "string", Required: true, APIPath: "name"},
			{Name: "delete_protection", Type: "bool", Optional: true, Computed: true,
				StringBool: true, APIPath: "metaData.delete_protection"},
		},
	}
	if err := spec.validate(); err != nil {
		t.Fatalf("stringBool spec rejected: %v", err)
	}
	r := &dynamicResource{spec: spec}
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)

	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":                tftypes.String,
		"name":              tftypes.String,
		"delete_protection": tftypes.Bool,
	}}
	bodyFor := func(protect tftypes.Value) map[string]any {
		raw := tftypes.NewValue(objType, map[string]tftypes.Value{
			"id":                tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			"name":              tftypes.NewValue(tftypes.String, "rg-1"),
			"delete_protection": protect,
		})
		return r.bodyFromRaw(raw, "create", nil)
	}

	// false must serialize as the STRING "false" — a JSON bool would fail to
	// deserialize into the API's string-valued metadata map.
	body := bodyFor(tftypes.NewValue(tftypes.Bool, false))
	meta, ok := body["metaData"].(map[string]any)
	if !ok {
		t.Fatalf("metaData not built as an object: %#v", body["metaData"])
	}
	if got := meta["delete_protection"]; got != "false" {
		t.Errorf("delete_protection = %#v (%T), want string \"false\"", got, got)
	}

	body = bodyFor(tftypes.NewValue(tftypes.Bool, true))
	meta, _ = body["metaData"].(map[string]any)
	if got := meta["delete_protection"]; got != "true" {
		t.Errorf("delete_protection = %#v (%T), want string \"true\"", got, got)
	}

	// Unset must send nothing at all, so the platform applies its own default
	// (delete protection on) rather than the provider silently disabling it.
	body = bodyFor(tftypes.NewValue(tftypes.Bool, nil))
	if _, present := body["metaData"]; present {
		t.Errorf("unset delete_protection still sent metaData: %#v", body["metaData"])
	}
}

// The read direction parses the string back to a bool. Only an explicit "true"
// is true, matching how the platform reads these keys.
func TestStringBool_ResponseParsesStringToBool(t *testing.T) {
	a := AttributeSpec{Name: "delete_protection", Type: "bool", Optional: true, Computed: true,
		StringBool: true, APIPath: "metaData.delete_protection"}

	cases := []struct {
		data     any
		wantNull bool
		want     bool
	}{
		{"true", false, true},
		{"True", false, true},   // case-insensitive
		{"TRUE", false, true},   // case-insensitive
		{"false", false, false}, // explicit off
		{"", false, false},      // any other value is off
		{"yes", false, false},   // not "true" → off
		{true, false, true},     // tolerate a real bool
		{false, false, false},
		{nil, true, false}, // key absent → null, so a dropped value shows as drift
	}
	for _, tc := range cases {
		got := attrFromResponse(a, tftypes.Bool, tc.data)
		if tc.wantNull {
			if !got.IsNull() {
				t.Errorf("data %#v: expected null, got %v", tc.data, got)
			}
			continue
		}
		var b bool
		if err := got.As(&b); err != nil {
			t.Fatalf("data %#v: As() failed: %v", tc.data, err)
		}
		if b != tc.want {
			t.Errorf("data %#v -> %v, want %v", tc.data, b, tc.want)
		}
	}
}

// The schema must expose a real bool, not a string — that is the whole point of
// carrying the flag rather than declaring the attribute as a string.
func TestStringBool_SchemaTypeIsBool(t *testing.T) {
	s := attrSchema(AttributeSpec{Name: "delete_protection", Type: "bool",
		Optional: true, Computed: true, StringBool: true})
	if got := s.GetType(); got != types.BoolType {
		t.Errorf("schema type = %v, want types.BoolType", got)
	}
}

// stringBool is only meaningful on a bool, and it cannot be combined with
// updateBoolTrueValue since both rewrite the same value's wire form. Both must be
// caught at spec load, not silently ignored.
func TestStringBool_SpecValidation(t *testing.T) {
	base := func(a AttributeSpec) error {
		s := ResourceSpec{
			Name: "x", IDPath: "id",
			Endpoint:   EndpointSpec{UriBase: "/x"},
			Attributes: []AttributeSpec{a},
		}
		return s.validate()
	}
	if err := base(AttributeSpec{Name: "flag", Type: "string", Optional: true, StringBool: true,
		APIPath: "metaData.flag"}); err == nil {
		t.Error("expected error for stringBool on a string attribute")
	}
	if err := base(AttributeSpec{Name: "flag", Type: "int", Optional: true, StringBool: true,
		APIPath: "metaData.flag"}); err == nil {
		t.Error("expected error for stringBool on an int attribute")
	}
	if err := base(AttributeSpec{Name: "flag", Type: "bool", Optional: true, StringBool: true,
		UpdateBoolTrueValue: "IMMUTABLE", UpdatePath: "x", APIPath: "metaData.flag"}); err == nil {
		t.Error("expected error when stringBool is combined with updateBoolTrueValue")
	}
	if err := base(AttributeSpec{Name: "flag", Type: "bool", Optional: true, Computed: true,
		StringBool: true, APIPath: "metaData.flag"}); err != nil {
		t.Errorf("valid stringBool attribute rejected: %v", err)
	}
}
