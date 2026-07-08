package duplocloud

import (
	"context"
	"testing"
	"time"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

func TestIsNonEmptyValue(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want bool
	}{
		{"nil", nil, false},
		{"empty string", "", false},
		{"string", "x", true},
		{"empty slice", []any{}, false},
		{"slice", []any{1}, true},
		{"empty map", map[string]any{}, false},
		{"map", map[string]any{"a": 1}, true},
		{"false bool", false, false},
		{"true bool", true, true},
		{"number", float64(0), true},
	}
	for _, c := range cases {
		if got := isNonEmptyValue(c.v); got != c.want {
			t.Errorf("%s: isNonEmptyValue(%v)=%v, want %v", c.name, c.v, got, c.want)
		}
	}
}

// The populated gate must hold success until the configured path is non-empty,
// mirroring a load balancer address that the cloud assigns after the resource is
// already Complete.
func TestApplyPopulatedGate_HoldsUntilPopulated(t *testing.T) {
	w := &duplosdk.Waiter[map[string]any]{
		PollInterval: time.Millisecond,
		SuccessState: "Complete",
		StatusFn:     func(m *map[string]any) string { s, _ := (*m)["status"].(string); return s },
	}
	applyPopulatedGate(w, "result.lb.ingress")

	n := 0
	obj, err := w.Wait(context.Background(), "x", time.Second, func() (*map[string]any, duplosdk.ClientError) {
		n++
		// Complete on poll 2, but the LB ingress list only appears on poll 4.
		m := map[string]any{"status": "Pending", "result": map[string]any{"lb": map[string]any{}}}
		if n >= 2 {
			m["status"] = "Complete"
		}
		if n >= 4 {
			m["result"] = map[string]any{"lb": map[string]any{"ingress": []any{map[string]any{"hostname": "a.elb.aws"}}}}
		}
		return &m, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n < 4 {
		t.Errorf("waiter returned before load balancer populated: calls=%d", n)
	}
	if obj == nil {
		t.Fatal("nil result")
	}
}

// With an existing readyPath gate, the populated gate composes: BOTH must hold.
func TestApplyPopulatedGate_ComposesWithExistingReadyGate(t *testing.T) {
	w := &duplosdk.Waiter[map[string]any]{
		PollInterval: time.Millisecond,
		SuccessState: "Complete",
		StatusFn:     func(m *map[string]any) string { s, _ := (*m)["status"].(string); return s },
		ReadyState:   "running",
		ReadyFn:      func(m *map[string]any) string { s, _ := (*m)["live"].(string); return s },
	}
	applyPopulatedGate(w, "lb")

	n := 0
	_, err := w.Wait(context.Background(), "x", time.Second, func() (*map[string]any, duplosdk.ClientError) {
		n++
		m := map[string]any{"status": "Complete", "live": "", "lb": []any{}}
		if n >= 3 {
			m["live"] = "running" // prior ready gate now met
		}
		if n >= 5 {
			m["lb"] = []any{"addr"} // populated gate now met
		}
		return &m, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n < 5 {
		t.Errorf("waiter returned before both gates met: calls=%d", n)
	}
}
