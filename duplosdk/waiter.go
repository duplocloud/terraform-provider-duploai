package duplosdk

import (
	"context"
	"fmt"
	"log"
	"time"
)

// Waiter polls a resource until a terminal state is reached.
// Configure once per resource kind; reuse across Create and Update waits.
//
// Inspired by boto3's Waiter model: success/failure states are declared up
// front, the actual polling loop is generic, and callers only supply a
// fetch function — they never write the loop themselves.
type Waiter[T any] struct {
	PollInterval    time.Duration
	SuccessState    string
	FailureStates   map[string]string // status value → human-readable reason
	StatusFn        func(*T) string
	FailureDetailFn func(*T) string // optional: extra context appended to the error message
}

// Wait polls fetchFn until the resource reaches SuccessState, a FailureState,
// timeout elapses, or ctx is cancelled. name is used only for log messages.
func (w *Waiter[T]) Wait(ctx context.Context, name string, timeout time.Duration, fetchFn func() (*T, ClientError)) (*T, ClientError) {
	log.Printf("[TRACE] waiter(%s): start (timeout=%s)", name, timeout)
	deadline := time.Now().Add(timeout)
	for {
		obj, err := fetchFn()
		if err != nil {
			return nil, err
		}
		status := w.StatusFn(obj)
		log.Printf("[TRACE] waiter(%s): status=%s", name, status)

		if status == w.SuccessState {
			return obj, nil
		}
		if reason, bad := w.FailureStates[status]; bad {
			msg := fmt.Sprintf("%s (status: %q)", reason, status)
			if w.FailureDetailFn != nil {
				if detail := w.FailureDetailFn(obj); detail != "" {
					msg += ": " + detail
				}
			}
			return nil, newClientError(0, fmt.Errorf("%s", msg))
		}
		if time.Now().After(deadline) {
			return nil, newClientError(0, fmt.Errorf("timed out waiting for %s (last status: %q)", name, status))
		}

		// Sleep between polls, but wake immediately if the operation is
		// cancelled (e.g. the user interrupts terraform apply).
		select {
		case <-ctx.Done():
			return nil, newClientError(0, fmt.Errorf("waiting for %s cancelled: %w", name, ctx.Err()))
		case <-time.After(w.PollInterval):
		}
	}
}

// WaitGone polls fetchFn until it reports the resource is gone (a NotFound
// error), a FailureState is reached (e.g. DeprovisionFailed), timeout elapses,
// or ctx is cancelled. Use it after issuing an asynchronous delete to confirm
// deprovisioning actually completed.
func (w *Waiter[T]) WaitGone(ctx context.Context, name string, timeout time.Duration, fetchFn func() (*T, ClientError)) ClientError {
	log.Printf("[TRACE] waiter(%s): waiting for deletion (timeout=%s)", name, timeout)
	deadline := time.Now().Add(timeout)
	for {
		obj, err := fetchFn()
		if err != nil {
			if err.IsNotFound() {
				return nil // gone
			}
			return err
		}
		status := w.StatusFn(obj)
		log.Printf("[TRACE] waiter(%s): still present, status=%s", name, status)

		if reason, bad := w.FailureStates[status]; bad {
			msg := fmt.Sprintf("%s (status: %q)", reason, status)
			if w.FailureDetailFn != nil {
				if detail := w.FailureDetailFn(obj); detail != "" {
					msg += ": " + detail
				}
			}
			return newClientError(0, fmt.Errorf("%s", msg))
		}
		if time.Now().After(deadline) {
			return newClientError(0, fmt.Errorf("timed out waiting for %s to be deleted (last status: %q)", name, status))
		}

		select {
		case <-ctx.Done():
			return newClientError(0, fmt.Errorf("waiting for %s deletion cancelled: %w", name, ctx.Err()))
		case <-time.After(w.PollInterval):
		}
	}
}

// WaitDeprovisioned polls fetchFn until the resource reaches successState (the
// terminal deprovisioned value) or is gone (a NotFound error), erroring on a
// FailureState (e.g. DeprovisionFailed), timeout, or ctx cancellation. Use it
// between a deprovision call and the final delete.
func (w *Waiter[T]) WaitDeprovisioned(ctx context.Context, name, successState string, timeout time.Duration, fetchFn func() (*T, ClientError)) ClientError {
	log.Printf("[TRACE] waiter(%s): waiting for deprovision to %q (timeout=%s)", name, successState, timeout)
	deadline := time.Now().Add(timeout)
	for {
		obj, err := fetchFn()
		if err != nil {
			if err.IsNotFound() {
				return nil // already gone — nothing left to deprovision
			}
			return err
		}
		status := w.StatusFn(obj)
		log.Printf("[TRACE] waiter(%s): deprovision status=%s", name, status)

		if status == successState {
			return nil
		}
		if reason, bad := w.FailureStates[status]; bad {
			msg := fmt.Sprintf("%s (status: %q)", reason, status)
			if w.FailureDetailFn != nil {
				if detail := w.FailureDetailFn(obj); detail != "" {
					msg += ": " + detail
				}
			}
			return newClientError(0, fmt.Errorf("%s", msg))
		}
		if time.Now().After(deadline) {
			return newClientError(0, fmt.Errorf("timed out waiting for %s to deprovision (last status: %q)", name, status))
		}

		select {
		case <-ctx.Done():
			return newClientError(0, fmt.Errorf("waiting for %s deprovision cancelled: %w", name, ctx.Err()))
		case <-time.After(w.PollInterval):
		}
	}
}
