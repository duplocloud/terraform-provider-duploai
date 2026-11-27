package duplocloud

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func conflictsErrors(t *testing.T, deferUnknown bool, a, b tftypes.Value) int {
	t.Helper()
	spec := ResourceSpec{
		ConflictsWith:             [][]string{{"a", "b"}},
		ConflictsWithDeferUnknown: deferUnknown,
		Attributes: []AttributeSpec{
			{Name: "a", Type: "list(string)", Optional: true},
			{Name: "b", Type: "list(string)", Optional: true},
		},
	}
	lt := tftypes.List{ElementType: tftypes.String}
	raw := tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{"a": lt, "b": lt}},
		map[string]tftypes.Value{"a": a, "b": b})
	req := resource.ValidateConfigRequest{Config: tfsdk.Config{Raw: raw}}
	resp := &resource.ValidateConfigResponse{}
	conflictsWithValidator{spec: spec}.ValidateResource(context.Background(), req, resp)
	return resp.Diagnostics.ErrorsCount()
}

func TestConflictsWithDeferUnknown(t *testing.T) {
	lt := tftypes.List{ElementType: tftypes.String}
	known := tftypes.NewValue(lt, []tftypes.Value{tftypes.NewValue(tftypes.String, "x")})
	unknown := tftypes.NewValue(lt, tftypes.UnknownValue)
	null := tftypes.NewValue(lt, nil)

	if n := conflictsErrors(t, false, unknown, known); n == 0 {
		t.Error("default: unknown must count as set")
	}
	if n := conflictsErrors(t, true, unknown, known); n != 0 {
		t.Errorf("defer: wholly unknown must be skipped, got %d errors", n)
	}
	if n := conflictsErrors(t, true, known, known); n == 0 {
		t.Error("defer: two known values must still conflict")
	}
	if n := conflictsErrors(t, true, known, null); n != 0 {
		t.Error("defer: one set one null is fine")
	}
	partial := tftypes.NewValue(lt, []tftypes.Value{tftypes.NewValue(tftypes.String, tftypes.UnknownValue)})
	if n := conflictsErrors(t, true, partial, known); n == 0 {
		t.Error("defer: known list with unknown element is set")
	}
}
