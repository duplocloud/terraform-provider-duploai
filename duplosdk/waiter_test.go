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
