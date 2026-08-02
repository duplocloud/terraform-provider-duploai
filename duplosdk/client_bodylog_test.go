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
		`"administratorLoginPassword":"Sup3rSecret!pg",` +
		`"authConfig":{"tenantId":"abc"},` +
		`"credentials":[{"key":"token","value":"dahp_realtoken"}],` +
		`"apiKey":"ghp_realkey","storageSizeGB":32}}`)

	got := redactBody(in)

	for _, secret := range []string{"Sup3rSecret!pg", "ghp_realkey"} {
		if strings.Contains(got, secret) {
			t.Errorf("secret leaked into the log: %q\n%s", secret, got)
		}
	}
	// Non-secret fields must survive, or the log is useless for debugging.
	if !strings.Contains(got, `"administratorLogin":"pgadmin"`) {
		t.Errorf("non-secret field was redacted:\n%s", got)
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
