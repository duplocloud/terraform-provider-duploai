package duplocloud

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var rwcType = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
	"encryption_mode": tftypes.String, "kms_master_key_id": tftypes.String, "visibility": tftypes.Number,
}}

func rwcObj(mode, key, vis tftypes.Value) tftypes.Value {
	return tftypes.NewValue(rwcType, map[string]tftypes.Value{"encryption_mode": mode, "kms_master_key_id": key, "visibility": vis})
}

func rwcStr(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }

var (
	rwcNullStr  = tftypes.NewValue(tftypes.String, nil)
	rwcUnknown  = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	rwcVis30    = tftypes.NewValue(tftypes.Number, 30)
	rwcVis45    = tftypes.NewValue(tftypes.Number, 45)
	rwcPriorKey = "alias/aws/sqs"
)

// run applies the modifier as Terraform does after UseStateForUnknown kept the
// prior key in the plan, and returns the planned key.
func rwcRun(t *testing.T, state, plan, config tftypes.Value, configKey tftypes.Value) types.String {
	t.Helper()
	req := planmodifier.StringRequest{
		State:       tfsdk.State{Raw: state},
		Plan:        tfsdk.Plan{Raw: plan},
		Config:      tfsdk.Config{Raw: config},
		ConfigValue: types.StringNull(),
		StateValue:  types.StringValue(rwcPriorKey),
		PlanValue:   types.StringValue(rwcPriorKey),
	}
	if !configKey.IsNull() {
		var s string
		_ = configKey.As(&s)
		req.ConfigValue = types.StringValue(s)
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	recomputeWhenChangedModifier{inputs: []string{"encryption_mode"}}.PlanModifyString(context.Background(), req, resp)
	return resp.PlanValue
}

func TestRecomputeWhenChanged(t *testing.T) {
	state := rwcObj(rwcStr("SseKms"), rwcStr(rwcPriorKey), rwcVis30)
	cases := []struct {
		name        string
		state       tftypes.Value
		plan        tftypes.Value
		config      tftypes.Value
		configKey   tftypes.Value
		wantUnknown bool
	}{
		// TC15: the mode changes and the key is not configured → recompute.
		{"mode changed, key unset", state, rwcObj(rwcStr("SseKmsRg"), rwcStr(rwcPriorKey), rwcVis30), rwcObj(rwcStr("SseKmsRg"), rwcNullStr, rwcVis30), rwcNullStr, true},
		// An explicit key is the user's choice: never override it.
		{"mode changed, key set", state, rwcObj(rwcStr("SseKmsRg"), rwcStr("arn:k"), rwcVis30), rwcObj(rwcStr("SseKmsRg"), rwcStr("arn:k"), rwcVis30), rwcStr("arn:k"), false},
		// An unrelated update keeps the prior key (no churn, nothing new sent).
		{"unrelated change", state, rwcObj(rwcStr("SseKms"), rwcStr(rwcPriorKey), rwcVis45), rwcObj(rwcStr("SseKms"), rwcNullStr, rwcVis45), rwcNullStr, false},
		// encryption_mode unset in config shows unknown in the proposed plan; its
		// own UseStateForUnknown keeps it, so it is unchanged.
		{"mode unset in config", state, rwcObj(rwcUnknown, rwcStr(rwcPriorKey), rwcVis45), rwcObj(rwcNullStr, rwcNullStr, rwcVis45), rwcNullStr, false},
		// The mode set from a value not known until apply may change it.
		{"mode from unknown reference", state, rwcObj(rwcUnknown, rwcStr(rwcPriorKey), rwcVis30), rwcObj(rwcUnknown, rwcNullStr, rwcVis30), rwcNullStr, true},
		// Create has no prior value to undo.
		{"create", tftypes.NewValue(rwcType, nil), rwcObj(rwcStr("SseKmsRg"), rwcUnknown, rwcVis30), rwcObj(rwcStr("SseKmsRg"), rwcNullStr, rwcVis30), rwcNullStr, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rwcRun(t, tc.state, tc.plan, tc.config, tc.configKey)
			if got.IsUnknown() != tc.wantUnknown {
				t.Errorf("planned key = %v, want unknown=%v", got, tc.wantUnknown)
			}
		})
	}
}

func TestRecomputeWhenChangedValidation(t *testing.T) {
	cases := []struct{ name, attrs, wantErr string }{
		{"valid", `{"name":"mode","type":"string","optional":true,"computed":true,"apiPath":"m"},{"name":"key","type":"string","optional":true,"computed":true,"apiPath":"k","recomputeWhenChanged":["mode"]}`, ""},
		{"unknown input", `{"name":"key","type":"string","optional":true,"computed":true,"apiPath":"k","recomputeWhenChanged":["nope"]}`, "references unknown attribute"},
		{"not computed", `{"name":"mode","type":"string","optional":true,"apiPath":"m"},{"name":"key","type":"string","optional":true,"apiPath":"k","recomputeWhenChanged":["mode"]}`, "optional+computed string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var spec ResourceSpec
			if err := json.Unmarshal([]byte(`{"name":"probe","description":"probe","idPath":"id","endpoint":{"uriBase":"/v3/things"},"attributes":[`+tc.attrs+`]}`), &spec); err != nil {
				t.Fatal(err)
			}
			err := spec.validate()
			if tc.wantErr == "" && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("want %q, got %v", tc.wantErr, err)
			}
		})
	}
}
