package duplosdk

import (
	"strings"
	"testing"
)

// Body logging is a local-debugging aid, so it must be OFF unless explicitly
// switched on — bodies routinely carry credentials.
func TestBodyLogging_DisabledByDefault(t *testing.T) {
	for _, v := range []string{"", "0", "false", "no", "  "} {
		t.Setenv(bodyLogEnv, v)
		if bodyLoggingEnabled() {
			t.Errorf("body logging must stay off for %q", v)
		}
	}
	for _, v := range []string{"1", "true", "yes", "on"} {
		t.Setenv(bodyLogEnv, v)
		if !bodyLoggingEnabled() {
			t.Errorf("body logging should be on for %q", v)
		}
	}
}

// Secret-looking values must never reach the log, whatever resource they belong
// to — the redaction keys off the JSON field name, not a known schema.
func TestRedactBody_HidesSecrets(t *testing.T) {
	in := []byte(`{"spec":{"administratorLogin":"pgadmin",` +
		`"administratorLoginPassword":"PLACEHOLDER-pg-password",` +
		`"authConfig":{"tenantId":"abc"},` +
		`"credentials":[{"key":"token","value":"PLACEHOLDER-provider-token"},` +
		`{"key":"certificateAuthorityData","value":"PLACEHOLDER-ca-data"},` +
		`{"key":"iamRoleArn","value":"arn:aws:iam::1234:role/NotASecret"}],` +
		`"apiKey":"PLACEHOLDER-api-key","storageSizeGB":32}}`)

	got := redactBody(in)

	for _, secret := range []string{
		"PLACEHOLDER-pg-password",
		"PLACEHOLDER-api-key",
		// Typed key/value pairs: the secret sits under a field named "value",
		// so a field-name match alone never fires — this is the case that
		// leaked provider credentials before the walk-the-document rewrite.
		"PLACEHOLDER-provider-token",
		"PLACEHOLDER-ca-data",
	} {
		if strings.Contains(got, secret) {
			t.Errorf("secret leaked into the log: %q\n%s", secret, got)
		}
	}
	// Non-secret fields must survive, or the log is useless for debugging.
	if !strings.Contains(got, `"administratorLogin":"pgadmin"`) {
		t.Errorf("non-secret field was redacted:\n%s", got)
	}
	// The credential's KEY name stays readable — you need to know which field
	// was sent — and a non-secret typed value is not blanked.
	if !strings.Contains(got, `"token"`) {
		t.Errorf("credential key name should stay visible:\n%s", got)
	}
	if !strings.Contains(got, "arn:aws:iam::1234:role/NotASecret") {
		t.Errorf("a non-secret typed value must not be redacted:\n%s", got)
	}
	if !strings.Contains(got, `"storageSizeGB":32`) {
		t.Errorf("numeric field was mangled:\n%s", got)
	}
}

func TestRedactBody_TruncatesAndHandlesEmpty(t *testing.T) {
	if got := redactBody(nil); got != "(empty)" {
		t.Errorf("empty body = %q, want (empty)", got)
	}
	big := []byte(`{"data":"` + strings.Repeat("x", maxLoggedBody*2) + `"}`)
	got := redactBody(big)
	if len(got) > maxLoggedBody+80 {
		t.Errorf("logged body not truncated: %d bytes", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncation not signposted:\n%s", got[:120])
	}
}

// A body that is not JSON (an HTML error page, a plain message) still gets the
// field-name pass rather than being logged verbatim or dropped.
func TestRedactBody_NonJSONFallsBackToPatternMatch(t *testing.T) {
	in := []byte(`<html>oops "password":"PLACEHOLDER-leak" </html>`)
	got := redactBody(in)
	if strings.Contains(got, "PLACEHOLDER-leak") {
		t.Errorf("secret leaked from a non-JSON body:\n%s", got)
	}
	if !strings.Contains(got, "<html>") {
		t.Errorf("non-JSON body should still be readable:\n%s", got)
	}
}
