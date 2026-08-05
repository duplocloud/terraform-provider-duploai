package duplocloud

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// InvalidWhen exists for the cross-field rules requiredIf and conflictsWith cannot
// express: numeric bounds, a comparison between two attributes, and a rule between
// leaves of the same object. The autoscaler bounds and upgrade policy of
// azure_node_pool are all three.
func iwNum(v float64) *float64 { return &v }

// defRaw builds an attribute default the way a spec file would.
func iwDefault(s string) *json.RawMessage { r := json.RawMessage(s); return &r }

func invalidWhenSpec() ResourceSpec {
	return ResourceSpec{
		Name:     "pool",
		IDPath:   "id",
		Endpoint: EndpointSpec{UriBase: "/pools"},
		Attributes: []AttributeSpec{
			{Name: "enable_auto_scaling", Type: "bool", Optional: true, Computed: true,
				Default: iwDefault(`false`), APIPath: "spec.enableAutoScaling"},
			{Name: "min_count", Type: "int", Optional: true, Computed: true,
				Default: iwDefault(`1`), APIPath: "spec.minCount"},
			{Name: "max_count", Type: "int", Optional: true, Computed: true,
				Default: iwDefault(`1`), APIPath: "spec.maxCount"},
			{Name: "upgrade_settings", Type: "object", Optional: true, Computed: true,
				APIPath: "spec.upgradeSettings", Attributes: []AttributeSpec{
					{Name: "max_surge_type", Type: "string", Optional: true, Computed: true,
						Default: iwDefault(`"Default"`), APIPath: "maxSurgeType"},
					{Name: "max_surge_value", Type: "int", Optional: true, Computed: true, APIPath: "maxSurgeValue"},
					{Name: "max_unavailable_type", Type: "string", Optional: true, Computed: true,
						Default: iwDefault(`"Default"`), APIPath: "maxUnavailableType"},
					{Name: "max_unavailable_value", Type: "int", Optional: true, Computed: true, APIPath: "maxUnavailableValue"},
				}},
		},
		InvalidWhen: []InvalidWhenRule{
			{
				Attribute: "min_count",
				When: []RequiredIfCondition{
					{Attribute: "enable_auto_scaling", Equals: "true"},
					{Attribute: "min_count", LessThan: iwNum(1)},
				},
				Message: "min_count must be at least 1 when enable_auto_scaling is true.",
			},
			{
				Attribute: "max_count",
				When: []RequiredIfCondition{
					{Attribute: "enable_auto_scaling", Equals: "true"},
					{Attribute: "max_count", LessThanAttribute: "min_count"},
				},
				Message: "max_count must be >= min_count when enable_auto_scaling is true.",
			},
			{
				Attribute: "upgrade_settings",
				When: []RequiredIfCondition{
					{Attribute: "upgrade_settings.max_surge_type", NotEquals: "Default"},
					{Attribute: "upgrade_settings.max_surge_value", GreaterThan: iwNum(0)},
					{Attribute: "upgrade_settings.max_unavailable_type", NotEquals: "Default"},
					{Attribute: "upgrade_settings.max_unavailable_value", GreaterThan: iwNum(0)},
				},
				Message: "upgrade_settings cannot have both surge and unavailable active.",
			},
		},
	}
}

var upgradeObjType = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
	"max_surge_type":        tftypes.String,
	"max_surge_value":       tftypes.Number,
	"max_unavailable_type":  tftypes.String,
	"max_unavailable_value": tftypes.Number,
}}

var poolObjType = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
	"enable_auto_scaling": tftypes.Bool,
	"min_count":           tftypes.Number,
	"max_count":           tftypes.Number,
	"upgrade_settings":    upgradeObjType,
}}

// poolConfig builds a config value. A nil pointer means the attribute is absent, so
// the rule has to fall back to the spec default the way a real plan does.
func poolConfig(autoscale *bool, minCount, maxCount *int64, upgrade map[string]tftypes.Value) tftypes.Value {
	num := func(v *int64) tftypes.Value {
		if v == nil {
			return tftypes.NewValue(tftypes.Number, nil)
		}
		return tftypes.NewValue(tftypes.Number, *v)
	}
	as := tftypes.NewValue(tftypes.Bool, nil)
	if autoscale != nil {
		as = tftypes.NewValue(tftypes.Bool, *autoscale)
	}
	up := tftypes.NewValue(upgradeObjType, nil)
	if upgrade != nil {
		up = tftypes.NewValue(upgradeObjType, upgrade)
	}
	return tftypes.NewValue(poolObjType, map[string]tftypes.Value{
		"enable_auto_scaling": as,
		"min_count":           num(minCount),
		"max_count":           num(maxCount),
		"upgrade_settings":    up,
	})
}

func upgradeValue(surgeType string, surgeVal *int64, unavailType string, unavailVal *int64) map[string]tftypes.Value {
	num := func(v *int64) tftypes.Value {
		if v == nil {
			return tftypes.NewValue(tftypes.Number, nil)
		}
		return tftypes.NewValue(tftypes.Number, *v)
	}
	return map[string]tftypes.Value{
		"max_surge_type":        tftypes.NewValue(tftypes.String, surgeType),
		"max_surge_value":       num(surgeVal),
		"max_unavailable_type":  tftypes.NewValue(tftypes.String, unavailType),
		"max_unavailable_value": num(unavailVal),
	}
}

func runInvalidWhen(t *testing.T, raw tftypes.Value) []string {
	t.Helper()
	spec := invalidWhenSpec()
	if err := spec.validateInvalidWhen(); err != nil {
		t.Fatalf("spec must be valid: %v", err)
	}
	r := &dynamicResource{spec: spec}
	schemaResp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, schemaResp)
	req := resource.ValidateConfigRequest{
		Config: tfsdk.Config{Raw: raw, Schema: schemaResp.Schema},
	}
	resp := &resource.ValidateConfigResponse{}
	invalidWhenValidator{spec: spec}.ValidateResource(context.Background(), req, resp)
	var msgs []string
	for _, d := range resp.Diagnostics.Errors() {
		msgs = append(msgs, d.Detail())
	}
	return msgs
}

func iwBool(b bool) *bool  { return &b }
func iwInt(i int64) *int64 { return &i }

func TestInvalidWhen_AutoscalerBounds(t *testing.T) {
	tests := []struct {
		name     string
		raw      tftypes.Value
		wantHit  string
		wantNone bool
	}{
		{
			name:    "min below one while autoscaling",
			raw:     poolConfig(iwBool(true), iwInt(0), iwInt(3), nil),
			wantHit: "min_count must be at least 1",
		},
		{
			name:     "min below one but autoscaling off",
			raw:      poolConfig(iwBool(false), iwInt(0), iwInt(3), nil),
			wantNone: true,
		},
		{
			name:     "min below one with autoscaling left at its default of false",
			raw:      poolConfig(nil, iwInt(0), iwInt(3), nil),
			wantNone: true,
		},
		{
			name:    "max below min while autoscaling",
			raw:     poolConfig(iwBool(true), iwInt(3), iwInt(2), nil),
			wantHit: "max_count must be >= min_count",
		},
		{
			// The trap this rule is really for: max_count is absent, so it defaults to
			// 1 — below the min the user set. Comparing against the default is what
			// catches it before apply.
			name:    "min set, max left at its default",
			raw:     poolConfig(iwBool(true), iwInt(3), nil, nil),
			wantHit: "max_count must be >= min_count",
		},
		{
			name:     "equal bounds are fine",
			raw:      poolConfig(iwBool(true), iwInt(2), iwInt(2), nil),
			wantNone: true,
		},
		{
			name:     "valid range",
			raw:      poolConfig(iwBool(true), iwInt(1), iwInt(5), nil),
			wantNone: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runInvalidWhen(t, tt.raw)
			if tt.wantNone {
				if len(got) != 0 {
					t.Errorf("expected no error, got %v", got)
				}
				return
			}
			joined := strings.Join(got, " | ")
			if !strings.Contains(joined, tt.wantHit) {
				t.Errorf("expected an error mentioning %q, got %v", tt.wantHit, got)
			}
		})
	}
}

func TestInvalidWhen_UpgradeSettingsMutualExclusion(t *testing.T) {
	tests := []struct {
		name     string
		upgrade  map[string]tftypes.Value
		wantNone bool
	}{
		{
			name:    "both active is rejected",
			upgrade: upgradeValue("NodeCount", iwInt(2), "NodeCount", iwInt(1)),
		},
		{
			name:     "surge only",
			upgrade:  upgradeValue("Percentage", iwInt(33), "Default", iwInt(0)),
			wantNone: true,
		},
		{
			name:     "unavailable only",
			upgrade:  upgradeValue("Default", iwInt(0), "NodeCount", iwInt(1)),
			wantNone: true,
		},
		{
			// Matches the API exactly: a non-Default type with a zero value is inert,
			// so this pair is accepted even though both types are set.
			name:     "both types set but one value is zero",
			upgrade:  upgradeValue("NodeCount", iwInt(2), "NodeCount", iwInt(0)),
			wantNone: true,
		},
		{
			// Also matches the API: Default ignores its value entirely.
			name:     "Default type with a stray value",
			upgrade:  upgradeValue("Default", iwInt(5), "NodeCount", iwInt(1)),
			wantNone: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runInvalidWhen(t, poolConfig(iwBool(false), nil, nil, tt.upgrade))
			if tt.wantNone {
				if len(got) != 0 {
					t.Errorf("expected no error, got %v", got)
				}
				return
			}
			if len(got) == 0 || !strings.Contains(strings.Join(got, " "), "both surge and unavailable") {
				t.Errorf("expected the mutual-exclusion error, got %v", got)
			}
		})
	}
}

// An unresolved reference to another resource must not be judged: it is configured,
// just not known yet. Reporting it invalid would break a perfectly good plan.
func TestInvalidWhen_UnknownValueIsNotJudged(t *testing.T) {
	raw := tftypes.NewValue(poolObjType, map[string]tftypes.Value{
		"enable_auto_scaling": tftypes.NewValue(tftypes.Bool, true),
		"min_count":           tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
		"max_count":           tftypes.NewValue(tftypes.Number, 2),
		"upgrade_settings":    tftypes.NewValue(upgradeObjType, nil),
	})
	if got := runInvalidWhen(t, raw); len(got) != 0 {
		t.Errorf("an unknown value must not be reported invalid, got %v", got)
	}
}

func TestInvalidWhen_Validation(t *testing.T) {
	base := invalidWhenSpec()
	tests := []struct {
		name    string
		mutate  func(s *ResourceSpec)
		wantErr string
	}{
		{"valid", func(*ResourceSpec) {}, ""},
		{
			"unknown attribute",
			func(s *ResourceSpec) {
				s.InvalidWhen = []InvalidWhenRule{{
					When:    []RequiredIfCondition{{Attribute: "nope", Equals: "x"}},
					Message: "m",
				}}
			},
			"unknown attribute",
		},
		{
			// A leaf inside a collection of objects needs an index or key in the
			// framework path, which a dot-path cannot carry — so it must be rejected at
			// startup rather than producing a rule that never fires.
			"leaf inside a list(object)",
			func(s *ResourceSpec) {
				s.Attributes = append(s.Attributes, AttributeSpec{
					Name: "taints", Type: "list(object)", Optional: true, APIPath: "spec.taints",
					Attributes: []AttributeSpec{{Name: "effect", Type: "string", Required: true, APIPath: "effect"}},
				})
				s.InvalidWhen = []InvalidWhenRule{{
					When:    []RequiredIfCondition{{Attribute: "taints.effect", Equals: "NoExecute"}},
					Message: "m",
				}}
			},
			"unknown attribute",
		},
		{
			"unknown nested leaf",
			func(s *ResourceSpec) {
				s.InvalidWhen = []InvalidWhenRule{{
					When:    []RequiredIfCondition{{Attribute: "upgrade_settings.nope", Equals: "x"}},
					Message: "m",
				}}
			},
			"unknown attribute",
		},
		{
			"no message",
			func(s *ResourceSpec) {
				s.InvalidWhen = []InvalidWhenRule{{
					When: []RequiredIfCondition{{Attribute: "min_count", LessThan: iwNum(1)}},
				}}
			},
			"no message",
		},
		{
			"no condition",
			func(s *ResourceSpec) {
				s.InvalidWhen = []InvalidWhenRule{{Message: "m"}}
			},
			"no condition",
		},
		{
			"two operators",
			func(s *ResourceSpec) {
				s.InvalidWhen = []InvalidWhenRule{{
					When:    []RequiredIfCondition{{Attribute: "min_count", LessThan: iwNum(1), GreaterThan: iwNum(5)}},
					Message: "m",
				}}
			},
			"exactly one operator",
		},
		{
			"numeric operator on a string attribute",
			func(s *ResourceSpec) {
				s.InvalidWhen = []InvalidWhenRule{{
					When:    []RequiredIfCondition{{Attribute: "upgrade_settings.max_surge_type", GreaterThan: iwNum(0)}},
					Message: "m",
				}}
			},
			"numeric operator on a string attribute",
		},
		{
			"lessThanAttribute pointing at a string",
			func(s *ResourceSpec) {
				s.InvalidWhen = []InvalidWhenRule{{
					When: []RequiredIfCondition{
						{Attribute: "min_count", LessThanAttribute: "upgrade_settings.max_surge_type"},
					},
					Message: "m",
				}}
			},
			"not numeric",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base
			s.InvalidWhen = append([]InvalidWhenRule(nil), base.InvalidWhen...)
			s.Attributes = append([]AttributeSpec(nil), base.Attributes...)
			tt.mutate(&s)
			err := s.validateInvalidWhen()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("want an error mentioning %q, got nil", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("error = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}
