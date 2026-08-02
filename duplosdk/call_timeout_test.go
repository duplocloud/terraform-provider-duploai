package duplosdk

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A slow operation-initiating call must survive past the client's default
// deadline when the caller asks for more room. Before per-call timeouts, an
// Azure teardown that ran longer than the 60s default was cut off — and because
// the backend runs it synchronously, the disconnect CANCELLED the teardown
// rather than just losing the reply.
func TestWithCallTimeout_LongCallSurvivesDefaultDeadline(t *testing.T) {
	const serverDelay = 250 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(serverDelay)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Default deadline far shorter than the server takes.
	client, err := NewClient(srv.URL, "tok", false, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ep := Endpoint{UriBase: "/things", Deprovision: Operation{Verb: http.MethodPost, Path: "/{id}/deprovision"}}
	api := NewRESTResource[map[string]any](client, ep, map[string]string{}, nil)

	// Without an override the default applies and the call is cut off.
	if err := api.Deprovision("o1"); err == nil {
		t.Fatal("expected the default deadline to cut the call off")
	} else if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected a deadline error, got: %v", err)
	}

	// With the operation's own timeout it completes.
	if err := api.WithCallTimeout(5 * time.Second).Deprovision("o1"); err != nil {
		t.Fatalf("call should have survived with a longer deadline: %v", err)
	}
}

// WithCallTimeout must not mutate the shared resource — the polling GETs the
// waiter issues keep the default deadline.
func TestWithCallTimeout_DoesNotMutateReceiver(t *testing.T) {
	client, err := NewClient("http://example.invalid", "tok", false, 7*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	api := NewRESTResource[map[string]any](client, Endpoint{UriBase: "/things"}, map[string]string{}, nil)

	long := api.WithCallTimeout(9 * time.Minute)
	if api.callTimeout != 0 {
		t.Errorf("receiver was mutated: callTimeout = %s", api.callTimeout)
	}
	if long.callTimeout != 9*time.Minute {
		t.Errorf("copy = %s, want 9m", long.callTimeout)
	}
	// A non-positive override is a no-op rather than "no deadline at all".
	if got := api.WithCallTimeout(0); got != api {
		t.Error("a zero timeout should return the receiver unchanged")
	}
}

// The per-call deadline can only widen the window, never narrow it — a caller
// passing something shorter than the configured default must not make ordinary
// calls flakier than the operator asked for.
func TestWithCallTimeout_NeverShortensTheDefault(t *testing.T) {
	const serverDelay = 200 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(serverDelay)
		_, _ = w.Write([]byte(`{"data":{"id":"o1"}}`))
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	api := NewRESTResource[map[string]any](client, Endpoint{UriBase: "/things"}, map[string]string{}, nil)

	if _, err := api.WithCallTimeout(1 * time.Millisecond).Get("o1"); err != nil {
		t.Fatalf("a shorter override must fall back to the 5s default, got: %v", err)
	}
}

// A Client built literally rather than via NewClient has a zero timeout. Before
// the floor, every context deadline was already expired and the first call died
// with a misleading "context deadline exceeded".
func TestZeroValueClientTimeout_FallsBackToDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":"o1"}}`))
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), HostURL: srv.URL} // no timeout set
	api := NewRESTResource[map[string]any](c, Endpoint{UriBase: "/things"}, map[string]string{}, nil)

	if _, err := api.Get("o1"); err != nil {
		t.Fatalf("a zero-value client must fall back to the default deadline, got: %v", err)
	}
}

// Reads must keep the client default even on a resource widened for a long
// operation: the waiter polls through the same resource, and a wedged GET should
// retry on the next poll rather than hold the operation's whole deadline open.
func TestWithCallTimeout_ReadsKeepTheDefaultDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			time.Sleep(200 * time.Millisecond)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"o1"}}`))
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "tok", false, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	api := NewRESTResource[map[string]any](client, Endpoint{UriBase: "/things"}, map[string]string{}, nil).
		WithCallTimeout(5 * time.Second)

	// The write inherits the widened deadline...
	if _, err := api.Update("o1", &map[string]any{}); err != nil {
		t.Fatalf("write should use the widened deadline: %v", err)
	}
	// ...the read does not.
	if _, err := api.Get("o1"); err == nil {
		t.Fatal("reads must stay on the client default, not the widened deadline")
	}
}
