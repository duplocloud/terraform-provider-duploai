package duplosdk

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

var errNotFound = fmt.Errorf("not found")

func statusWaiter() *Waiter[map[string]any] {
	return &Waiter[map[string]any]{
		PollInterval:    time.Millisecond,
		SuccessState:    "Ready",
		FailureStates:   map[string]string{"Failed": "provisioning failed"},
		StatusFn:        func(m *map[string]any) string { s, _ := (*m)["s"].(string); return s },
		FailureDetailFn: func(m *map[string]any) string { d, _ := (*m)["detail"].(string); return d },
	}
}

func TestWaiter_SucceedsAfterPolling(t *testing.T) {
	n := 0
	obj, err := statusWaiter().Wait(context.Background(), "x", time.Second, func() (*map[string]any, ClientError) {
		n++
		s := "Pending"
		if n >= 3 {
			s = "Ready"
		}
		return &map[string]any{"s": s}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if (*obj)["s"] != "Ready" || n < 3 {
		t.Errorf("obj=%v calls=%d", *obj, n)
	}
}

// The readiness gate holds success until BOTH status==SuccessState and the
// ready value matches — e.g. status Ready but live not yet "running".
func TestWaiter_ReadyGate(t *testing.T) {
	w := statusWaiter()
	w.ReadyState = "running"
	w.ReadyFn = func(m *map[string]any) string { s, _ := (*m)["live"].(string); return s }
	n := 0
	obj, err := w.Wait(context.Background(), "x", time.Second, func() (*map[string]any, ClientError) {
		n++
		// Status reaches Ready on poll 2, but live_state only becomes running on poll 4.
		s, live := "Pending", ""
		if n >= 2 {
			s = "Ready"
		}
		if n >= 4 {
			live = "running"
		}
		return &map[string]any{"s": s, "live": live}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n < 4 {
		t.Errorf("waiter returned before ready gate met: calls=%d", n)
	}
	if (*obj)["live"] != "running" {
		t.Errorf("final live=%v, want running", (*obj)["live"])
	}
}

// A failure status still aborts even when a readiness gate is configured.
func TestWaiter_ReadyGate_FailureStillAborts(t *testing.T) {
	w := statusWaiter()
	w.ReadyState = "running"
	w.ReadyFn = func(m *map[string]any) string { s, _ := (*m)["live"].(string); return s }
	_, err := w.Wait(context.Background(), "x", time.Second, func() (*map[string]any, ClientError) {
		return &map[string]any{"s": "Failed", "detail": "boom"}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "provisioning failed") {
		t.Fatalf("expected failure abort, got %v", err)
	}
}

// ReadyFailureFn/ReadyFailureStates abort the wait immediately on a terminal
// value at the secondary signal, even though the primary status already
// reports success (e.g. a Flux HelmRelease CR applied fine but the chart
// install itself failed).
func TestWaiter_ReadyFailureState_AbortsDespitePrimarySuccess(t *testing.T) {
	w := statusWaiter()
	w.ReadyState = "True"
	w.ReadyFn = func(m *map[string]any) string { s, _ := (*m)["readyStatus"].(string); return s }
	w.ReadyFailureStates = map[string]string{"InstallFailed": "Helm install failed"}
	w.ReadyFailureFn = func(m *map[string]any) string { s, _ := (*m)["readyReason"].(string); return s }
	_, err := w.Wait(context.Background(), "x", time.Second, func() (*map[string]any, ClientError) {
		return &map[string]any{"s": "Ready", "readyStatus": "False", "readyReason": "InstallFailed"}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "Helm install failed") || !strings.Contains(err.Error(), "InstallFailed") {
		t.Fatalf("expected ready-failure abort, got %v", err)
	}
}

// A ready-failure reason that isn't in ReadyFailureStates (e.g. a transient
// "Progressing" reason while Flux still reconciles) must not abort the wait.
func TestWaiter_ReadyFailureState_UnknownReasonKeepsPolling(t *testing.T) {
	w := statusWaiter()
	w.ReadyState = "True"
	w.ReadyFn = func(m *map[string]any) string { s, _ := (*m)["readyStatus"].(string); return s }
	w.ReadyFailureStates = map[string]string{"InstallFailed": "Helm install failed"}
	w.ReadyFailureFn = func(m *map[string]any) string { s, _ := (*m)["readyReason"].(string); return s }
	n := 0
	obj, err := w.Wait(context.Background(), "x", time.Second, func() (*map[string]any, ClientError) {
		n++
		readyStatus, readyReason := "False", "Progressing"
		if n >= 3 {
			readyStatus, readyReason = "True", "InstallSucceeded"
		}
		return &map[string]any{"s": "Ready", "readyStatus": readyStatus, "readyReason": readyReason}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if (*obj)["readyStatus"] != "True" || n < 3 {
		t.Errorf("obj=%v calls=%d", *obj, n)
	}
}

func TestWaiter_FailureState(t *testing.T) {
	_, err := statusWaiter().Wait(context.Background(), "x", time.Second, func() (*map[string]any, ClientError) {
		return &map[string]any{"s": "Failed", "detail": "quota exceeded"}, nil
	})
	if err == nil {
		t.Fatal("expected failure-state error")
	}
	if !strings.Contains(err.Error(), "provisioning failed") || !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("error missing reason/detail: %v", err)
	}
}

// FailureRetries: a transient failure status that recovers within the allowed
// retries should not abort the wait — the resource still succeeds.
func TestWaiter_TransientFailureRecovers(t *testing.T) {
	w := statusWaiter()
	w.FailureRetries = 3
	n := 0
	// Pending → Failed → Failed → Pending → Ready (Failed seen twice, within 3 retries).
	seq := []string{"Pending", "Failed", "Failed", "Pending", "Ready"}
	obj, err := w.Wait(context.Background(), "x", time.Second, func() (*map[string]any, ClientError) {
		s := seq[n]
		n++
		return &map[string]any{"s": s}, nil
	})
	if err != nil {
		t.Fatalf("transient failure within retries should recover, got error: %v", err)
	}
	if (*obj)["s"] != "Ready" {
		t.Errorf("expected Ready, got %v", *obj)
	}
}

// FailureRetries: a failure status that persists beyond the allowed retries is
// still terminal.
func TestWaiter_PersistentFailureAborts(t *testing.T) {
	w := statusWaiter()
	w.FailureRetries = 2
	n := 0
	_, err := w.Wait(context.Background(), "x", time.Second, func() (*map[string]any, ClientError) {
		n++
		return &map[string]any{"s": "Failed", "detail": "still broken"}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "provisioning failed") {
		t.Fatalf("expected failure after retries exhausted, got %v", err)
	}
	if n != 3 { // first observation + 2 retries, then abort
		t.Errorf("expected 3 polls (1 + 2 retries), got %d", n)
	}
}

func TestWaiter_Timeout(t *testing.T) {
	_, err := statusWaiter().Wait(context.Background(), "x", time.Nanosecond, func() (*map[string]any, ClientError) {
		return &map[string]any{"s": "Pending"}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error, got %v", err)
	}
}

func TestWaiterGone_GoneAfterPolling(t *testing.T) {
	n := 0
	err := statusWaiter().WaitGone(context.Background(), "x", time.Second, func() (*map[string]any, ClientError) {
		n++
		if n < 3 {
			return &map[string]any{"s": "Deprovisioning"}, nil
		}
		return nil, newClientError(404, errNotFound) // gone
	})
	if err != nil {
		t.Fatalf("expected nil once gone, got %v", err)
	}
	if n < 3 {
		t.Errorf("polled %d times, expected to wait for deletion", n)
	}
}

func TestWaiterGone_ImmediateNotFound(t *testing.T) {
	err := statusWaiter().WaitGone(context.Background(), "x", time.Second, func() (*map[string]any, ClientError) {
		return nil, newClientError(404, errNotFound)
	})
	if err != nil {
		t.Errorf("already-gone should return nil, got %v", err)
	}
}

func TestWaiterGone_FailureState(t *testing.T) {
	w := statusWaiter()
	w.FailureStates = map[string]string{"DeprovisionFailed": "deprovisioning failed"}
	err := w.WaitGone(context.Background(), "x", time.Second, func() (*map[string]any, ClientError) {
		return &map[string]any{"s": "DeprovisionFailed"}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "deprovisioning failed") {
		t.Errorf("expected deprovision failure, got %v", err)
	}
}

func TestWaiterGone_Timeout(t *testing.T) {
	err := statusWaiter().WaitGone(context.Background(), "x", time.Nanosecond, func() (*map[string]any, ClientError) {
		return &map[string]any{"s": "Deprovisioning"}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout, got %v", err)
	}
}

func TestWaiterGone_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := statusWaiter()
	w.PollInterval = time.Hour
	err := w.WaitGone(ctx, "x", time.Hour, func() (*map[string]any, ClientError) {
		return &map[string]any{"s": "Deprovisioning"}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected cancellation error, got %v", err)
	}
}

// #5 — a cancelled context aborts the poll loop promptly, even with a long
// PollInterval, instead of sleeping until the next tick.
func TestWaiter_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the first inter-poll sleep

	w := statusWaiter()
	w.PollInterval = time.Hour // would hang for an hour if ctx were ignored

	done := make(chan ClientError, 1)
	go func() {
		_, err := w.Wait(ctx, "x", time.Hour, func() (*map[string]any, ClientError) {
			return &map[string]any{"s": "Pending"}, nil
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Errorf("expected cancellation error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after context cancellation")
	}
}

// A read that fails says nothing about the resource, so the waiter must retry
// rather than abandon a create that is succeeding — the Azure Postgres case,
// where GET blocks past the HTTP deadline while the backend applies firewall
// rules, and the server goes Ready moments later.
func TestWaiter_RetriesTransientFetchErrors(t *testing.T) {
	calls := 0
	w := &Waiter[map[string]any]{
		PollInterval: time.Millisecond,
		SuccessState: "Complete",
		FetchRetries: 3,
		StatusFn:     func(m *map[string]any) string { return (*m)["status"].(string) },
	}
	obj, err := w.Wait(context.Background(), "pg", time.Minute, func() (*map[string]any, ClientError) {
		calls++
		if calls <= 2 { // two deadline-exceeded polls, then the real answer
			return nil, newClientError(0, fmt.Errorf("context deadline exceeded"))
		}
		return &map[string]any{"status": "Complete"}, nil
	})
	if err != nil {
		t.Fatalf("wait failed despite retries available: %v", err)
	}
	if (*obj)["status"] != "Complete" {
		t.Errorf("status = %v, want Complete", (*obj)["status"])
	}
	if calls != 3 {
		t.Errorf("fetch calls = %d, want 3", calls)
	}
}

// Past the retry budget the error still surfaces — retries must not mask a
// backend that is genuinely unreachable.
func TestWaiter_GivesUpAfterFetchRetries(t *testing.T) {
	calls := 0
	w := &Waiter[map[string]any]{
		PollInterval: time.Millisecond,
		SuccessState: "Complete",
		FetchRetries: 2,
		StatusFn:     func(m *map[string]any) string { return (*m)["status"].(string) },
	}
	_, err := w.Wait(context.Background(), "pg", time.Minute, func() (*map[string]any, ClientError) {
		calls++
		return nil, newClientError(0, fmt.Errorf("context deadline exceeded"))
	})
	if err == nil {
		t.Fatal("expected the error to surface once retries are exhausted")
	}
	if calls != 3 { // initial attempt + 2 retries
		t.Errorf("fetch calls = %d, want 3", calls)
	}
}
