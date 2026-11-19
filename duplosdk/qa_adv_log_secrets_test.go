package duplosdk

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflogtest"
)

// adv-qa F-4. The Azure Key Vault secret body carries the secret (legacy
// `value` AND the write-only `value_wo`) under a plain JSON key "value" with
// no secret-named sibling. With DUPLO_LOG_HTTP_BODIES on, redactBody must
// still keep it out of the log — a write-only argument promises the value is
// never persisted, and a TF_LOG=trace file is persisted.
func TestQAAdv_BodyLog_KeyVaultSecretValueRedacted(t *testing.T) {
	t.Setenv(bodyLogEnv, "1")
	const secret = "PLACEHOLDER-kv-secret-value"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"name":"s1","contentType":"text/plain"}}`))
	}))
	defer srv.Close()
	c, err := NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRESTResource[map[string]any](c, Endpoint{UriBase: "/v1/kv/secrets"}, nil, nil)
	var out bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &out)
	body := map[string]any{"name": "s1", "value": secret, "contentType": "text/plain"}
	if _, cerr := r.WithContext(ctx).Create(&body); cerr != nil {
		t.Fatal(cerr)
	}
	if !strings.Contains(out.String(), "duplo-request-body") {
		t.Fatalf("body logging did not run; log:\n%s", out.String())
	}
	if strings.Contains(out.String(), secret) {
		t.Errorf("key vault secret value reached the provider log in clear text (plain \"value\" key is not redacted)")
	}
}

// adv-qa F-4. A non-2xx error body is TRACE-logged raw, whatever
// DUPLO_LOG_HTTP_BODIES says. If the backend (or a cloud SDK error it relays)
// echoes a secret-named field, the log must not carry it in clear text.
func TestQAAdv_ErrorBodyLog_RedactsSecretFields(t *testing.T) {
	t.Setenv(bodyLogEnv, "")
	const secret = "PLACEHOLDER-echoed-password"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"title":"validation","spec":{"masterUserPassword":"` + secret + `"}}`))
	}))
	defer srv.Close()
	c, err := NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRESTResource[map[string]any](c, Endpoint{UriBase: "/v1/rds"}, nil, nil)
	var out bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &out)
	body := map[string]any{"spec": map[string]any{"masterUserPassword": secret}}
	_, cerr := r.WithContext(ctx).Create(&body)
	if cerr == nil {
		t.Fatal("want a 400 error")
	}
	if !strings.Contains(out.String(), "error body") {
		t.Fatalf("error body line not logged; log:\n%s", out.String())
	}
	if strings.Contains(out.String(), secret) {
		t.Errorf("secret echoed in a non-2xx error body is TRACE-logged unredacted (body logging off)")
	}
}
