package duplocloud

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// k8sLabelValue mirrors the rule the platform enforces on an allocation tag.
const k8sLabelValue = `^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`

// runStringValidators reports the first validation error for value, or "".
func runStringValidators(t *testing.T, a AttributeSpec, value string) string {
	t.Helper()
	attr, ok := attrSchema(a).(schema.StringAttribute)
	if !ok {
		t.Fatalf("attrSchema(%q) is not a StringAttribute", a.Name)
	}
	for _, v := range attr.Validators {
		req := validator.StringRequest{ConfigValue: types.StringValue(value)}
		var resp validator.StringResponse
		v.ValidateString(context.Background(), req, &resp)
		if resp.Diagnostics.HasError() {
			return resp.Diagnostics.Errors()[0].Detail()
		}
	}
	return ""
}

// A value the API would reject must fail at plan time rather than as a 400
// mid-apply — that is the whole point of declaring the constraint.
func TestStringPatternRejectsInvalidValues(t *testing.T) {
	a := AttributeSpec{
		Name: "allocation_tags", Type: "string", Optional: true, Computed: true,
		Pattern:            k8sLabelValue,
		PatternDescription: "must be a valid Kubernetes label value",
		MaxLength:          63,
	}

	for _, tc := range []struct {
		name, value string
		wantErr     bool
	}{
		{"simple tag", "batch", false},
		{"dashes and dots", "shared-pool.v2", false},
		{"underscores inside", "a_b", false},
		{"empty is allowed", "", false},
		{"leading dash", "-batch", true},
		{"trailing dot", "batch.", true},
		{"space", "shared pool", true},
		{"slash", "team/batch", true},
		{"too long", strings.Repeat("a", 64), true},
		{"exactly 63", strings.Repeat("a", 63), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := runStringValidators(t, a, tc.value) != ""
			if gotErr != tc.wantErr {
				t.Errorf("value %q: error=%v, want error=%v", tc.value, gotErr, tc.wantErr)
			}
		})
	}
}

// The raw regex is not actionable to a practitioner, so patternDescription must
// reach the message when set.
func TestStringPatternUsesDescriptionInMessage(t *testing.T) {
	a := AttributeSpec{
		Name: "tag", Type: "string", Optional: true,
		Pattern: k8sLabelValue, PatternDescription: "must be a valid Kubernetes label value",
	}
	msg := runStringValidators(t, a, "-bad")
	if !strings.Contains(msg, "must be a valid Kubernetes label value") {
		t.Errorf("message %q does not carry patternDescription", msg)
	}
}

// Pattern/maxLength are wired for strings only, so declaring them elsewhere
// would be validation the spec claims but never performs.
func TestPatternRejectedOnNonStringTypes(t *testing.T) {
	cases := []struct {
		name string
		attr AttributeSpec
	}{
		{"pattern on int", AttributeSpec{Name: "n", Type: "int", Optional: true, Pattern: "^a$"}},
		{"maxLength on list", AttributeSpec{Name: "l", Type: "list(string)", Optional: true, MaxLength: 5}},
		{"description without pattern", AttributeSpec{Name: "s", Type: "string", Optional: true, PatternDescription: "orphan"}},
		{"uncompilable pattern", AttributeSpec{Name: "s", Type: "string", Optional: true, Pattern: "([unclosed"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateAttributes([]AttributeSpec{tc.attr}); err == nil {
				t.Error("expected validateAttributes to reject, got nil")
			}
		})
	}
}

// The constraint must hold identically on the data source schema; one enforcing
// it and the other not would be a trap.
func TestStringPatternAppliesToDataSourceSchema(t *testing.T) {
	a := AttributeSpec{Name: "tag", Type: "string", Required: true, Pattern: k8sLabelValue, MaxLength: 63}
	got := len(stringValidators(a))
	if got != 2 {
		t.Errorf("stringValidators() = %d validators, want 2 (pattern + maxLength)", got)
	}
}
