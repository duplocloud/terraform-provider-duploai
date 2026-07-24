package duplocloud

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Read side: response objects returned in a non-canonical order are sorted by
// the key before they reach state.
func TestSortObjectsByKey(t *testing.T) {
	in := []any{
		map[string]any{"name": "b"},
		map[string]any{"name": "a"},
		map[string]any{"name": "c"},
	}
	out := sortObjectsByKey(in, "name")
	got := []string{
		out[0].(map[string]any)["name"].(string),
		out[1].(map[string]any)["name"].(string),
		out[2].(map[string]any)["name"].(string),
	}
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("expected [a b c], got %v", got)
	}
	// Single/empty lists and empty key are no-ops.
	if len(sortObjectsByKey([]any{map[string]any{"name": "z"}}, "name")) != 1 {
		t.Error("single-element list should pass through")
	}
}

// Plan side: the plan modifier reorders the planned config to the same canonical
// order, so prior state (sorted on read) and config compare equal.
func TestOrderByKeyModifier_PlanModifyList(t *testing.T) {
	ctx := context.Background()
	attrTypes := map[string]attr.Type{"name": types.StringType}
	objType := types.ObjectType{AttrTypes: attrTypes}
	mk := func(n string) types.Object {
		return types.ObjectValueMust(attrTypes, map[string]attr.Value{"name": types.StringValue(n)})
	}
	list := types.ListValueMust(objType, []attr.Value{mk("b"), mk("a")})

	req := planmodifier.ListRequest{PlanValue: list}
	resp := &planmodifier.ListResponse{PlanValue: list}
	orderByKeyModifier{key: "name"}.PlanModifyList(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diags: %v", resp.Diagnostics)
	}
	elems := resp.PlanValue.Elements()
	first := elems[0].(types.Object).Attributes()["name"].(types.String).ValueString()
	second := elems[1].(types.Object).Attributes()["name"].(types.String).ValueString()
	if first != "a" || second != "b" {
		t.Fatalf("expected planned order [a b], got [%s %s]", first, second)
	}

	// Null/unknown plan values are left alone.
	nullReq := planmodifier.ListRequest{PlanValue: types.ListNull(objType)}
	nullResp := &planmodifier.ListResponse{PlanValue: types.ListNull(objType)}
	orderByKeyModifier{key: "name"}.PlanModifyList(ctx, nullReq, nullResp)
	if !nullResp.PlanValue.IsNull() {
		t.Error("null plan value should stay null")
	}
}

// Spec validation: orderByKey must reference a nested string attribute on a
// list(object).
func TestValidate_OrderByKey(t *testing.T) {
	valid := []AttributeSpec{{
		Name: "subnets", Type: "list(object)", Optional: true,
		OrderByKey: "name",
		Attributes: []AttributeSpec{{Name: "name", Type: "string", Required: true}},
	}}
	if _, err := validateAttributes(valid); err != nil {
		t.Fatalf("valid orderByKey rejected: %v", err)
	}

	cases := map[string][]AttributeSpec{
		"non-list type": {{
			Name: "x", Type: "string", Optional: true, OrderByKey: "name",
		}},
		"unknown key": {{
			Name: "subnets", Type: "list(object)", Optional: true, OrderByKey: "missing",
			Attributes: []AttributeSpec{{Name: "name", Type: "string", Required: true}},
		}},
		"non-string key": {{
			Name: "subnets", Type: "list(object)", Optional: true, OrderByKey: "port",
			Attributes: []AttributeSpec{{Name: "port", Type: "int", Required: true}},
		}},
	}
	for name, attrs := range cases {
		if _, err := validateAttributes(attrs); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}
