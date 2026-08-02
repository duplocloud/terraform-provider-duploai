package duplocloud

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// A one-way boolean (Azure Key Vault purge protection) must be rejected at PLAN
// time when turned back off. The alternative — quietly keeping the old value —
// is not available: the plugin framework has no diff suppression, and returning
// a plan value that differs from a set config value makes Terraform reject the
// plan with "planned value ... does not match config value ...".
func TestImmutableOnceTrue_PlanModifier(t *testing.T) {
	for _, tc := range []struct {
		name      string
		state     types.Bool
		plan      types.Bool
		wantError bool
	}{
		{"create (no prior state)", types.BoolNull(), types.BoolValue(false), false},
		{"create, turning on", types.BoolNull(), types.BoolValue(true), false},
		{"staying off", types.BoolValue(false), types.BoolValue(false), false},
		{"turning on", types.BoolValue(false), types.BoolValue(true), false},
		{"staying on", types.BoolValue(true), types.BoolValue(true), false},
		{"unknown plan (computed)", types.BoolValue(true), types.BoolUnknown(), false},
		{"TURNING OFF", types.BoolValue(true), types.BoolValue(false), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := immutableOnceTrueModifier{attr: "enable_purge_protection"}
			req := planmodifier.BoolRequest{
				Path:        path.Root("enable_purge_protection"),
				StateValue:  tc.state,
				PlanValue:   tc.plan,
				ConfigValue: tc.plan,
			}
			var resp planmodifier.BoolResponse
			resp.PlanValue = tc.plan
			m.PlanModifyBool(context.Background(), req, &resp)

			if got := resp.Diagnostics.HasError(); got != tc.wantError {
				t.Fatalf("HasError = %v, want %v (diags: %v)", got, tc.wantError, resp.Diagnostics)
			}
			// The planned value is never rewritten — doing so would make Terraform
			// reject the plan outright.
			if !resp.PlanValue.Equal(tc.plan) {
				t.Errorf("plan value was rewritten to %v; it must stay %v", resp.PlanValue, tc.plan)
			}
			if tc.wantError {
				detail := resp.Diagnostics.Errors()[0].Detail()
				if !strings.Contains(detail, "ignore_changes") {
					t.Errorf("the error should point at the escape hatch:\n%s", detail)
				}
			}
		})
	}
}

// The flag only makes sense on a bool, and is redundant on a forceNew attribute
// (which recreates on any change, so the plan-time check is unreachable).
func TestImmutableOnceTrue_Validation(t *testing.T) {
	for _, tc := range []struct {
		name string
		attr AttributeSpec
		ok   bool
	}{
		{"bool", AttributeSpec{Name: "b", Type: "bool", Optional: true, Computed: true, ImmutableOnceTrue: true}, true},
		{"string", AttributeSpec{Name: "s", Type: "string", Optional: true, ImmutableOnceTrue: true}, false},
		{"int", AttributeSpec{Name: "i", Type: "int", Optional: true, ImmutableOnceTrue: true}, false},
		{"with forceNew", AttributeSpec{Name: "b", Type: "bool", Optional: true, ForceNew: true, ImmutableOnceTrue: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateAttributes([]AttributeSpec{tc.attr})
			if tc.ok && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Error("expected a validation error, got nil")
			}
		})
	}
}
