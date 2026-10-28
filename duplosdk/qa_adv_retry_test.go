package duplosdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func qaAdv404Resource(t *testing.T) (*RESTResource[map[string]any], *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return NewRESTResource[map[string]any](c, Endpoint{UriBase: "/v1/items"}, nil, nil), &hits
}

// adv-qa retry scenario: a persistent 404 is retried for the window and no
// longer — the loop never sleeps past its deadline — and the last error is a
// not-found the caller can recognise.
func TestQAAdv_GetWithRetry_404BoundedByWindow(t *testing.T) {
	r, hits := qaAdv404Resource(t)
	const window = 3500 * time.Millisecond
	start := time.Now()
	_, err := r.GetWithRetry(context.Background(), "x", window)
	el := time.Since(start)
	if err == nil || !err.IsNotFound() {
		t.Fatalf("want a not-found error, got %v", err)
	}
	if el > window+500*time.Millisecond {
		t.Errorf("GetWithRetry overran its %s window: %s", window, el)
	}
	// attempts at ~0s, 1s, 3s (then 3s+4s > window): 3 GETs
	if n := atomic.LoadInt32(hits); n < 2 || n > 4 {
		t.Errorf("unexpected number of attempts in %s: %d", window, n)
	}
}

// Ctrl-C (Terraform request context cancelled) mid-window must end the wait
// promptly, not after the full 60 s data-source window.
func TestQAAdv_GetWithRetry_CancelEndsWait(t *testing.T) {
	r, hits := qaAdv404Resource(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	_, err := r.GetWithRetry(ctx, "x", 60*time.Second)
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("cancelled context did not end the retry wait promptly: %s", el)
	}
	if err == nil {
		t.Errorf("want the last error back after cancellation")
	}
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Errorf("want exactly 1 GET before cancellation, got %d", n)
	}

	// Already-cancelled context: one attempt, no wait.
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	start = time.Now()
	_, _ = r.GetWithRetry(ctx2, "x", 60*time.Second)
	if el := time.Since(start); el > time.Second {
		t.Errorf("already-cancelled context still waited %s", el)
	}
}
