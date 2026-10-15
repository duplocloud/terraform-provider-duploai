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

func newCtxTestResource(t *testing.T) (*RESTResource[map[string]any], *int) {
	t.Helper()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"data":{"id":"obj1","status":"Complete"}}`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return NewRESTResource[map[string]any](c, Endpoint{UriBase: "/v1/items"}, nil, nil), &hits
}

// The Terraform request context is used for its values (the tflog logger)
// only. A cancelled context — Ctrl-C — must not cut a backend call: a create
// the backend already received would otherwise leave an object Terraform never
// records. Backend calls behave exactly as before WithContext existed.
func TestWithContext_CancellationDoesNotCutRequests(t *testing.T) {
	r, hits := newCtxTestResource(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.WithContext(ctx).Get("obj1"); err != nil {
		t.Fatalf("a cancelled Terraform context must not cancel the backend call, got %v", err)
	}
	if _, err := r.WithContext(ctx).Create(&map[string]any{"name": "n"}); err != nil {
		t.Fatalf("create must still be sent and completed, got %v", err)
	}
	if *hits != 2 {
		t.Errorf("want both requests to reach the backend, got %d", *hits)
	}
}

// The SDK's request/response lines reach the provider log through tflog when a
// Terraform request context is attached.
func TestWithContext_SDKLogsThroughTflog(t *testing.T) {
	r, _ := newCtxTestResource(t)
	var out bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &out)
	if _, err := r.WithContext(ctx).Get("obj1"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"duplo-request: GET", "duplo-response: GET", "status=200"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("provider log missing %q; got:\n%s", want, out.String())
		}
	}
}

// The client sends the configured User-Agent; without one, Go's default goes
// out as before.
func TestClientUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got = req.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"data":{"id":"obj1"}}`))
	}))
	defer srv.Close()
	c, err := NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRESTResource[map[string]any](c, Endpoint{UriBase: "/v1/items"}, nil, nil)
	if _, err := r.Get("obj1"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "Go-http-client/") {
		t.Errorf("no UserAgent set: want Go's default, got %q", got)
	}
	c.UserAgent = "terraform-provider-duploai/1.2.3 (+x) Terraform/1.9.0"
	if _, err := r.Get("obj1"); err != nil {
		t.Fatal(err)
	}
	if got != c.UserAgent {
		t.Errorf("User-Agent = %q, want %q", got, c.UserAgent)
	}
}
