package duplocloud

import "testing"

// filterMapKeys drops backend-injected map entries (e.g. ALB annotations) so a
// map the user only partially manages doesn't show perpetual drift.
func TestFilterMapKeys(t *testing.T) {
	in := map[string]any{
		"nginx.ingress.kubernetes.io/rewrite-target":   "/",
		"alb.ingress.kubernetes.io/scheme":             "internet-facing", // user-set, NOT filtered
		"alb.ingress.kubernetes.io/security-groups":    "sg-1,sg-2",
		"alb.ingress.kubernetes.io/subnets":            "subnet-1",
		"alb.ingress.kubernetes.io/ssl-policy":         "ELBSecurityPolicy-TLS13-1-2-2021-06",
		"alb.ingress.kubernetes.io/target-node-labels": "resourcegroup=x",
		"alb.ingress.kubernetes.io/tags":               "duplocloud.ai/workspace=w",
		"alb.ingress.kubernetes.io/certificate-arn":    "arn:...:cert",
	}
	keys := []string{
		"alb.ingress.kubernetes.io/security-groups",
		"alb.ingress.kubernetes.io/subnets",
		"alb.ingress.kubernetes.io/certificate-arn",
		"alb.ingress.kubernetes.io/ssl-policy",
		"alb.ingress.kubernetes.io/target-node-labels",
		"alb.ingress.kubernetes.io/tags",
	}
	out, ok := filterMapKeys(in, keys, nil).(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}
	// Backend-injected keys dropped.
	for _, k := range keys {
		if _, present := out[k]; present {
			t.Errorf("key %q should have been filtered out", k)
		}
	}
	// User keys preserved (including a user-set alb.* key not in the filter list).
	for _, k := range []string{"nginx.ingress.kubernetes.io/rewrite-target", "alb.ingress.kubernetes.io/scheme"} {
		if _, present := out[k]; !present {
			t.Errorf("user key %q should be preserved", k)
		}
	}
	if len(out) != 2 {
		t.Errorf("expected 2 keys remaining, got %d", len(out))
	}

	// Non-map value passes through unchanged.
	if v := filterMapKeys("scalar", keys, nil); v != "scalar" {
		t.Errorf("non-map should pass through, got %v", v)
	}
	// Empty filter list is a no-op path (caller guards len>0, but be safe).
	if v, _ := filterMapKeys(map[string]any{"a": 1}, nil, nil).(map[string]any); len(v) != 1 {
		t.Errorf("nil keys should not drop anything")
	}
}

// Wildcard (prefix) filtering — "duplocloud.ai/*" drops all platform-stamped keys.
func TestFilterMapKeys_Wildcard(t *testing.T) {
	in := map[string]any{
		"app":                        "x",      // user key, kept
		"duplocloud.ai/workspace":    "w",      // dropped
		"duplocloud.ai/environment":  "e",      // dropped
		"duplocloud.ai/resourcetype": "K8sJob", // dropped
	}
	out, _ := filterMapKeys(in, []string{"duplocloud.ai/*", "resourcegroup"}, nil).(map[string]any)
	if len(out) != 1 || out["app"] != "x" {
		t.Errorf("wildcard filter should keep only user keys, got %v", out)
	}
}

// A key the user declares in config/state must survive filtering, even when it
// matches a filter pattern — otherwise the response never carries it back and
// the resource shows a perpetual (here: replacement-forcing) diff.
func TestFilterMapKeys_KeepsUserDeclaredKeys(t *testing.T) {
	in := map[string]any{
		"app":                     "x",
		"resourcegroup":           "u10-dev01", // stamped, but user-declared
		"duplocloud.ai/workspace": "w",         // stamped, not declared
	}
	keep := map[string]bool{"app": true, "resourcegroup": true}
	out, _ := filterMapKeys(in, []string{"duplocloud.ai/*", "resourcegroup"}, keep).(map[string]any)
	if len(out) != 2 {
		t.Fatalf("expected app + resourcegroup to survive, got %v", out)
	}
	if out["resourcegroup"] != "u10-dev01" {
		t.Errorf("user-declared resourcegroup should round-trip, got %v", out["resourcegroup"])
	}
	if _, present := out["duplocloud.ai/workspace"]; present {
		t.Error("undeclared stamped key should still be dropped")
	}
}
