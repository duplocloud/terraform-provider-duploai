package duplocloud

import "testing"

// resource_group updates over PUT, and the backend rebuilds its stored document from the
// request body — so a spec field the body omits is dropped from the record, not left alone.
// scope_ids is server-assigned (inherited from the linked network) and therefore
// computed-only, which makes bodyFromRaw skip it unless sendFromState is set.
//
// Without the flag, any update wiped spec.scopeIds. The resource then had no scopes to
// attach to its provisioning ticket, the ticket failed with "One or more scope IDs are not
// registered in workspace ...", and the resource group flipped to Failed. It surfaced when
// delete_protection gave callers their first routine reason to update a resource group
// (CUST-11952), but it applied to every update — changing description alone did it too.
//
// azure_managed_redis and azure_node_pool already carry the flag for the same reason; this
// test pins resource_group so it cannot regress by having the flag dropped again.
func TestResourceGroupSpec_ScopeIDsSurviveUpdate(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	var rg *ResourceSpec
	for i := range specs {
		if specs[i].Name == "resource_group" {
			rg = &specs[i]
			break
		}
	}
	if rg == nil {
		t.Fatal("resource_group spec not found")
	}

	a := rg.attr("scope_ids")
	if a == nil {
		t.Fatal("resource_group has no scope_ids attribute")
	}
	if a.Required || a.Optional {
		// If it ever becomes user-settable the body carries it anyway and this guard is moot,
		// but that is a deliberate design change and should be made knowingly.
		t.Skipf("scope_ids is no longer computed-only (required=%v optional=%v) — "+
			"revisit whether sendFromState is still the right mechanism", a.Required, a.Optional)
	}
	if !a.SendFromState {
		t.Error("resource_group.scope_ids is computed-only without sendFromState: " +
			"every PUT will omit spec.scopeIds and the backend will wipe it, failing provisioning")
	}
	if a.NoSend {
		t.Error("resource_group.scope_ids sets noSend, which keeps it out of the body entirely")
	}

	// The whole failure mode depends on the update being a full-document PUT. If the endpoint
	// ever moves to a merging PATCH this guard is still harmless, but the reasoning changes.
	if v := rg.Endpoint.Update; v != nil && v.Verb != "" && v.Verb != "PUT" {
		t.Logf("note: resource_group now updates via %s, not PUT — "+
			"re-check whether the backend still rebuilds the document from the body", v.Verb)
	}
}
