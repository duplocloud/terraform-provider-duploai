package duplosdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newServerClient spins up an httptest server and returns a Client pointed at it.
func newServerClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func newThings(c *Client) *RESTResource[map[string]any] {
	return NewRESTResource[map[string]any](c, Endpoint{UriBase: "/things"}, map[string]string{}, nil)
}

// #1 — a missing Data must surface as an error, never a nil pointer the engine
// would dereference.
func TestRESTResource_NilData_ReturnsError(t *testing.T) {
	bodies := map[string]string{
		"data is null":   `{"message":"ok","data":null}`,
		"no envelope":    `{"id":"x"}`,
		"empty body":     ``,
		"data is absent": `{"message":"ok"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			c := newServerClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			})
			r := newThings(c)

			if _, err := r.Create(&map[string]any{"name": "n"}); err == nil {
				t.Error("Create: expected error, got nil")
			}
			if _, err := r.Get("obj-1"); err == nil {
				t.Error("Get: expected error, got nil")
			}
			if _, err := r.Update("obj-1", &map[string]any{}); err == nil {
				t.Error("Update: expected error, got nil")
			}
		})
	}
}

func TestRESTResource_ValidEnvelope(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":"obj-1","name":"n"}}`))
	})
	r := newThings(c)

	got, err := r.Create(&map[string]any{"name": "n"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if (*got)["id"] != "obj-1" {
		t.Errorf("id = %v, want obj-1", (*got)["id"])
	}
}

func TestRESTResource_DeleteNotFoundIsSuccess(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if err := newThings(c).Delete("gone"); err != nil {
		t.Errorf("Delete on 404 should be nil, got %v", err)
	}
}

// Deprovision posts to the configured deprovision path (verb + {id}), and a 404
// is treated as success (already gone).
func TestRESTResource_Deprovision(t *testing.T) {
	var gotMethod, gotPath string
	c := newServerClient(t, func(w http.ResponseWriter, req *http.Request) {
		gotMethod, gotPath = req.Method, req.URL.Path
		_, _ = w.Write([]byte(`{"data":{}}`))
	})
	ep := Endpoint{
		UriBase:     "/things",
		Deprovision: Operation{Verb: http.MethodPost, Path: "/{id}/deprovision"},
	}
	r := NewRESTResource[map[string]any](c, ep, map[string]string{}, nil)
	if !ep.HasDeprovision() {
		t.Fatal("HasDeprovision should be true when a deprovision path is set")
	}
	if err := r.Deprovision("obj-1"); err != nil {
		t.Fatalf("Deprovision: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/things/obj-1/deprovision" {
		t.Errorf("got %s %s, want POST /things/obj-1/deprovision", gotMethod, gotPath)
	}

	c404 := newServerClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if err := NewRESTResource[map[string]any](c404, ep, map[string]string{}, nil).Deprovision("gone"); err != nil {
		t.Errorf("Deprovision on 404 should be nil, got %v", err)
	}
}

// WaitDeprovisioned returns once the resource reports the deprovisioned state,
// and errors on a terminal failure state.
func TestWaitDeprovisioned(t *testing.T) {
	w := &Waiter[map[string]any]{
		PollInterval:  time.Millisecond,
		FailureStates: map[string]string{"DeprovisionFailed": "deprovisioning failed"},
		StatusFn:      func(m *map[string]any) string { return (*m)["status"].(string) },
	}
	calls := 0
	err := w.WaitDeprovisioned(context.Background(), "x", "DeProvisioned", time.Second, func() (*map[string]any, ClientError) {
		calls++
		st := "DeProvisioning"
		if calls >= 2 {
			st = "DeProvisioned"
		}
		return &map[string]any{"status": st}, nil
	})
	if err != nil {
		t.Fatalf("WaitDeprovisioned: %v", err)
	}

	failErr := w.WaitDeprovisioned(context.Background(), "x", "DeProvisioned", time.Second, func() (*map[string]any, ClientError) {
		return &map[string]any{"status": "DeprovisionFailed"}, nil
	})
	if failErr == nil {
		t.Error("WaitDeprovisioned should error on DeprovisionFailed")
	}
}

// #6 — responses decode with UseNumber so large integers keep full precision
// (a float64 round-trip would corrupt 2^53+1).
func TestRESTResource_DecodesLargeIntAsJSONNumber(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"big":9007199254740993}}`))
	})
	got, err := newThings(c).Get("x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	n, ok := (*got)["big"].(json.Number)
	if !ok {
		t.Fatalf("big decoded as %T, want json.Number", (*got)["big"])
	}
	if n.String() != "9007199254740993" {
		t.Errorf("big = %s, want 9007199254740993", n.String())
	}
}
