package duplocloud

import "testing"

// Same failure mode as scope_ids (see scope_ids_send_from_state_test.go), one
// field further out: the update is a full-document write, so anything the body
// omits is written back as its default rather than left alone. Here the
// casualties are the record's LIFECYCLE fields.
//
// status and ever_completed are computed-only, so bodyFromRaw skips them and
// the PUT carries no status. ResourceStatus.New is the enum's zero value and
// EverCompleted defaults to false, and nothing on the backend's update path
// copies either from the stored row — so a metadata-only update knocked a
// Complete environment back to New with nothing to drive it forward again.
// Because the resource declares a waiter, the provider then polled for Complete
// until the update timeout: the write had landed, the apply failed anyway, and
// the next plan re-proposed the same change and timed out again (CUST-11951).
//
// Confirmed against a live backend three ways: a metadata-only PUT reset
// status on two separate environments, and the same PUT with status and
// everCompleted echoed back left the record Complete with the metadata applied.
// With the flag set, that update now completes in about a second.
//
// A genuine re-provision is unaffected: the backend ASSIGNS Status after
// deserialization when the spec changed, so the echoed value is overwritten
// exactly when it should be. Only the no-spec-change case — the metadata edit —
// keeps what the client sent, which is the prior value the provider holds.
//
// This is a workaround for a backend defect (a client payload should not be
// able to clear server-owned state). If the backend starts preserving these
// fields on update, this flag can go — but leaving it would then be harmless.
func TestLifecycleFieldsSurviveFullDocumentUpdate(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	byName := map[string]*ResourceSpec{}
	for i := range specs {
		byName[specs[i].Name] = &specs[i]
	}

	cases := map[string][]string{
		"environment":    {"status", "ever_completed"},
		"resource_group": {"status"},
	}
	for name, attrs := range cases {
		t.Run(name, func(t *testing.T) {
			s := byName[name]
			if s == nil {
				t.Fatalf("%s spec not found", name)
			}
			if s.Endpoint.Immutable {
				t.Skipf("%s is immutable again — no update body, so nothing can be reset", name)
			}
			if s.Waiter == nil {
				// Without a waiter a reset status is invisible rather than fatal, so the
				// flag matters less — but dropping the waiter is a deliberate change.
				t.Skipf("%s no longer declares a waiter — revisit whether this guard is needed", name)
			}
			for _, attr := range attrs {
				a := s.attr(attr)
				if a == nil {
					t.Errorf("%s has no %s attribute", name, attr)
					continue
				}
				if a.Required || a.Optional {
					t.Skipf("%s.%s is no longer computed-only (required=%v optional=%v) — "+
						"the body carries it anyway and this guard is moot", name, attr, a.Required, a.Optional)
				}
				if !a.SendFromState {
					t.Errorf("%s.%s is computed-only without sendFromState: the update body omits it, "+
						"the backend writes the zero value, and the waiter then blocks until the "+
						"update timeout on a change that already succeeded", name, attr)
				}
				if a.NoSend {
					t.Errorf("%s.%s sets noSend, which keeps it out of the body entirely", name, attr)
				}
			}
		})
	}
}

// The waiter watches `status`, so that is the attribute whose reset causes the
// hang. If a spec ever pointed its waiter elsewhere, this guard would be
// protecting the wrong field.
func TestWaiterWatchesTheAttributeWeEcho(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	for _, s := range specs {
		if s.Name != "environment" && s.Name != "resource_group" {
			continue
		}
		if s.Waiter == nil {
			continue
		}
		if got := s.Waiter.StatusPath; got != "status" {
			t.Errorf("%s waiter watches %q, not \"status\" — sendFromState is on the wrong attribute", s.Name, got)
		}
		if got := s.Waiter.SuccessState; got != "Complete" {
			t.Errorf("%s waiter succeeds on %q, not \"Complete\"", s.Name, got)
		}
	}
}
