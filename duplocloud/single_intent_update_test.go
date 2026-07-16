package duplocloud

import "testing"

func TestValidateSingleIntentUpdate(t *testing.T) {
	intentAttr := AttributeSpec{
		Name: "broker_count", Type: "int", Optional: true,
		UpdateIntent: &UpdateIntentSpec{DiscriminatorValue: "BrokerCount", ValuePath: "spec.updateRequest.targetNumberOfBrokerNodes"},
	}
	si := &SingleIntentUpdateSpec{DiscriminatorPath: "spec.updateRequest.updateType", ReadyPath: "result.cloudDetails.state", ReadyState: "ACTIVE"}

	cases := []struct {
		name    string
		spec    ResourceSpec
		wantErr bool
	}{
		{"valid", ResourceSpec{Attributes: []AttributeSpec{intentAttr}, SingleIntentUpdate: si}, false},
		{"no resource block", ResourceSpec{Attributes: []AttributeSpec{intentAttr}}, true},
		{"incomplete resource block", ResourceSpec{
			Attributes:         []AttributeSpec{intentAttr},
			SingleIntentUpdate: &SingleIntentUpdateSpec{DiscriminatorPath: "x"},
		}, true},
		{"incomplete intent", ResourceSpec{
			Attributes:         []AttributeSpec{{Name: "x", Type: "int", Optional: true, UpdateIntent: &UpdateIntentSpec{DiscriminatorValue: "BrokerCount"}}},
			SingleIntentUpdate: si,
		}, true},
		{"no intents, no block", ResourceSpec{Attributes: []AttributeSpec{{Name: "x", Type: "string", Optional: true}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSingleIntentUpdate(&tc.spec)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateSingleIntentUpdate err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

// jsonEqual underpins change detection in single-intent updates.
func TestJSONEqual(t *testing.T) {
	cases := []struct {
		a, b any
		want bool
	}{
		{int64(3), int64(3), true},
		{int64(3), int64(6), false},
		{"kafka.m5.large", "kafka.m5.large", true},
		{"kafka.m5.large", "kafka.m5.xlarge", false},
		{[]any{"a", "b"}, []any{"a", "b"}, true},
		{[]any{"a", "b"}, []any{"b", "a"}, false},
	}
	for _, tc := range cases {
		if got := jsonEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("jsonEqual(%v,%v)=%v want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
