package duplosdk

// DUPLOAI-4844: the client must honour HTTP_PROXY / HTTPS_PROXY / NO_PROXY.
//
// net/http.ProxyFromEnvironment reads the environment ONCE per process
// (envProxyFunc is guarded by sync.Once), so t.Setenv cannot reliably change
// proxy behaviour between tests in one process. Each scenario therefore runs
// the real client in a fresh child process (the test binary re-executed with
// -test.run=TestProxyChild and a scenario marker), with a controlled
// environment. The parent asserts observable behaviour: whether the local
// proxy server received the request.
//
// Note: Go never proxies loopback hosts, so the target is a non-resolvable
// host (duplo-api.invalid). Only a proxy can reach it.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const proxyChildEnv = "DUPLO_PROXY_TEST_CHILD"

// TestProxyChild is the child-process body; it is a no-op in the normal run.
func TestProxyChild(t *testing.T) {
	if os.Getenv(proxyChildEnv) != "1" {
		t.Skip("child helper; only runs when re-executed by a proxy test")
	}
	c, err := NewClient(os.Getenv("DUPLO_PROXY_TEST_HOST"), "tok", false, 5*time.Second)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	var out map[string]any
	if cerr := c.callAPIWithTimeout(context.Background(), 0, http.MethodGet, "/v1/ping", nil, &out); cerr != nil {
		t.Fatalf("CHILD_REQUEST_FAILED: %v", cerr)
	}
	if out["ok"] != true {
		t.Fatalf("CHILD_BAD_BODY: %v", out)
	}
}

// runProxyChild re-executes the test binary with a clean proxy environment
// plus extra, and returns combined output and whether the child passed.
func runProxyChild(t *testing.T, host string, extra ...string) (string, bool) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestProxyChild$", "-test.count=1", "-test.v")
	env := []string{proxyChildEnv + "=1", "DUPLO_PROXY_TEST_HOST=" + host}
	for _, kv := range os.Environ() {
		k := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		switch k {
		case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "REQUEST_METHOD":
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env, extra...)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func newRecordingProxy(t *testing.T) (*httptest.Server, *atomic.Int32, *atomic.Value) {
	var hits atomic.Int32
	var lastURL atomic.Value
	lastURL.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		lastURL.Store(r.URL.String()) // absolute-form URL when proxied
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits, &lastURL
}

// HTTP_PROXY set → request to an otherwise unreachable host goes via the proxy.
func TestProxy_HTTPProxyEnvRoutesRequests(t *testing.T) {
	proxy, hits, lastURL := newRecordingProxy(t)
	out, ok := runProxyChild(t, "http://duplo-api.invalid", "HTTP_PROXY="+proxy.URL)
	if hits.Load() == 0 {
		t.Fatalf("proxy was never used: HTTP_PROXY=%s ignored by client transport\nchild output:\n%s", proxy.URL, out)
	}
	if got := lastURL.Load().(string); got != "http://duplo-api.invalid/v1/ping" {
		t.Errorf("proxy saw URL %q, want absolute http://duplo-api.invalid/v1/ping", got)
	}
	if !ok {
		t.Fatalf("child request failed via proxy:\n%s", out)
	}
}

// NO_PROXY covering the host → client dials directly (proxy not hit), even with
// HTTP_PROXY set. The direct dial to .invalid must fail on DNS.
func TestProxy_NoProxyBypassesProxy(t *testing.T) {
	proxy, hits, _ := newRecordingProxy(t)
	out, ok := runProxyChild(t, "http://duplo-api.invalid",
		"HTTP_PROXY="+proxy.URL, "NO_PROXY=duplo-api.invalid")
	if hits.Load() != 0 {
		t.Fatalf("proxy was hit %d time(s) although NO_PROXY covers the host\n%s", hits.Load(), out)
	}
	if ok || !strings.Contains(out, "CHILD_REQUEST_FAILED") {
		t.Fatalf("expected a direct (failing) dial to duplo-api.invalid, got:\n%s", out)
	}
}

// Regression guard: no proxy env → direct request to the server, as today.
func TestProxy_NoEnvGoesDirect(t *testing.T) {
	direct, hits, lastURL := newRecordingProxy(t)
	out, ok := runProxyChild(t, direct.URL)
	if !ok || hits.Load() != 1 {
		t.Fatalf("direct request failed (hits=%d):\n%s", hits.Load(), out)
	}
	if got := lastURL.Load().(string); got != "/v1/ping" {
		t.Errorf("server saw %q, want origin-form /v1/ping (no proxy)", got)
	}
}
