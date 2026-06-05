package duplosdk

import (
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
// or timeout elapses. name is used only for log messages.
func (w *Waiter[T]) Wait(name string, timeout time.Duration, fetchFn func() (*T, ClientError)) (*T, ClientError) {
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
		time.Sleep(w.PollInterval)
	}
}
