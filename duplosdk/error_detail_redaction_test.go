package duplosdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A failed apply prints the API error to the user's terminal (and CI logs). When
// a 4xx body echoes the request — or a cloud SDK error relayed by the backend
// quotes a secret field — the error message must not carry the secret, exactly
// as the TRACE log line already does not. The raw body stays on the error so
// endpoint NotFound rules (asNotFound / errorBody) still match against it.
func TestErrorDetail_RedactsSecrets(t *testing.T) {
	const pw = "PLACEHOLDER-echoed-password"
	const val = "PLACEHOLDER-echoed-value"
	cases := []struct {
		name     string
		body     string
		endpoint Endpoint
		secret   string
	}{
		{
			name:     "secret-named field",
			body:     `{"title":"validation failed","spec":{"masterUserPassword":"` + pw + `"}}`,
			endpoint: Endpoint{UriBase: "/v1/rds"},
			secret:   pw,
		},
		{
			name:     "neutral field marked by the endpoint's SensitivePaths",
			body:     `{"title":"validation failed","value":"` + val + `"}`,
			endpoint: Endpoint{UriBase: "/v1/ssm", SensitivePaths: []string{"value"}},
			secret:   val,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c, err := NewClient(srv.URL, "tok", false, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			r := NewRESTResource[map[string]any](c, tc.endpoint, nil, nil)
			req := map[string]any{"name": "x"}
			_, cerr := r.WithContext(context.Background()).Create(&req)
			if cerr == nil {
				t.Fatal("want a 400 error")
			}
			msg := cerr.Error()
			if strings.Contains(msg, tc.secret) {
				t.Errorf("error message shows the secret in clear text: %s", msg)
			}
			if !strings.Contains(msg, "validation failed") {
				t.Errorf("error message lost the non-secret detail: %s", msg)
			}
			if !strings.Contains(errorBody(cerr), tc.secret) {
				t.Errorf("raw body must stay on the error for NotFound matching; got %q", errorBody(cerr))
			}
		})
	}
}

// An error message is not a log line: a long validation response must reach the
// user whole, not cut at the log cap.
func TestErrorDetail_NotTruncated(t *testing.T) {
	long := strings.Repeat("x", maxLoggedBody+500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":"` + long + `END"}`))
	}))
	defer srv.Close()
	c, err := NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRESTResource[map[string]any](c, Endpoint{UriBase: "/v1/things"}, nil, nil)
	req := map[string]any{"name": "x"}
	_, cerr := r.WithContext(context.Background()).Create(&req)
	if cerr == nil || !strings.Contains(cerr.Error(), "END") {
		t.Errorf("long error detail was truncated: %v", cerr)
	}
}
