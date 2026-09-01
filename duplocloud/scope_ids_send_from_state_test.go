package duplocloud

import "testing"

// Both of these resources update with a full-document write that REPLACES the stored spec —
// resource_group over PUT, k8s_namespace over PATCH — so a spec field the request body omits
// is dropped from the record rather than left alone. Confirmed against a live backend for
// each: a PUT/PATCH that leaves out spec.scopeIds reads back with scopeIds gone.
//
// scope_ids is server-assigned (inherited from the linked network / resource group) and so
// declared computed-only, which makes bodyFromRaw skip it unless sendFromState is set. Without
// the flag every update wiped spec.scopeIds; the resource then had no scopes to attach to its
// provisioning ticket, the ticket failed with "One or more scope IDs are not registered in
// workspace ...", and the resource flipped to Failed (CUST-11952).
//
// It surfaced when delete_protection gave callers their first routine reason to update these
// resources. For resource_group the defect was long-standing — it had always had an update
// path, so changing description alone did it too. For k8s_namespace the update path itself was
// new, so the flag has to land with it.
//
// azure_managed_redis and azure_node_pool already carry sendFromState for the same reason.
// This test pins the flag on both specs so it cannot be dropped again.
func TestScopeIDsSurviveFullDocumentUpdate(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	byName := map[string]*ResourceSpec{}
	for i := range specs {
		byName[specs[i].Name] = &specs[i]
	}

	for _, name := range []string{"resource_group", "k8s_namespace"} {
		t.Run(name, func(t *testing.T) {
			s := byName[name]
			if s == nil {
				t.Fatalf("%s spec not found", name)
			}
			// The failure mode only exists because the resource has an update path at all.
			if s.Endpoint.Immutable {
				t.Skipf("%s is immutable again — no update body, so nothing can be wiped", name)
			}
			a := s.attr("scope_ids")
			if a == nil {
				t.Fatalf("%s has no scope_ids attribute", name)
			}
			if a.Required || a.Optional {
				// User-settable means the body carries it anyway and this guard is moot, but
				// that is a deliberate design change and should be made knowingly.
				t.Skipf("%s.scope_ids is no longer computed-only (required=%v optional=%v) — "+
					"revisit whether sendFromState is still the right mechanism",
					name, a.Required, a.Optional)
			}
			if !a.SendFromState {
				t.Errorf("%s.scope_ids is computed-only without sendFromState: every update will "+
					"omit spec.scopeIds and the backend will wipe it, failing provisioning", name)
			}
			if a.NoSend {
				t.Errorf("%s.scope_ids sets noSend, which keeps it out of the body entirely", name)
			}
		})
	}
}
