package duplocloud

import "testing"

// scopeIDsFullReplaceSpecs are the mutable resources whose backend update replaces the stored
// record with the request body and does NOT restore spec.scopeIds from the existing record, so
// a body without scopeIds wipes them (F-14). Backend evidence, origin/dev: the generic path is
// ResourcesController.Update -> ResourceServiceBase.UpdateAsync -> MongoRepository.UpdateAsync
// (ReplaceOneAsync); ResourceGroupChildServiceBase stamps scopeIds from the resource group on
// CREATE only (PopulateExpandedPropertiesAsync, called from the create path).
//   - k8s_pvc, helm_repository, git_repository: K8sPassthroughServiceBase, no scopeIds restore.
//     k8s_pvc failed live with "inconsistent result: .scope_ids ... now null"; helm/git
//     repository had no UseStateForUnknown on scope_ids, so the same wipe passed silently
//     (plan "known after apply", null after apply).
//   - k8s_storage_class: ResourceGroupChildServiceBase, no OnBeforeUpdate; failed live as k8s_pvc.
//   - storage_account: AzurePassThroughServiceBase.OnBeforeUpdateAsync restores only
//     spec.uniqueCloudResourceId, not scopeIds.
var scopeIDsFullReplaceSpecs = []string{
	"resource_group", "k8s_namespace",
	"k8s_pvc", "k8s_storage_class", "helm_repository", "git_repository", "storage_account",
}

// scopeIDsRestampedOnUpdate are mutable specs with a computed-only scope_ids that is NOT sent,
// on purpose: their backend re-derives spec.scopeIds on every update, so a body without it
// cannot wipe it. Evidence, origin/dev:
//   - elasticache: AWSElastiCacheService.OnBeforeUpdateAsync re-runs
//     PopulateExpandedPropertiesAsync, which stamps scopeIds from the resource group when empty.
//   - cluster_attributes: ClusterAttributesService.OnBeforeUpdateAsync re-runs
//     PopulateExpandedPropertiesAsync, which rebuilds scopeIds from the parent cluster.
var scopeIDsRestampedOnUpdate = map[string]bool{
	"elasticache":        true,
	"cluster_attributes": true,
}

// TestScopeIDsComputedOnlySweep fails for any mutable spec with a computed-only scope_ids that
// is neither sent from state, nor explicitly kept out of the body (noSend), nor known to be
// re-derived by its backend on update. A new spec with that shape has to make the choice
// knowingly instead of silently wiping scopeIds on its first update.
func TestScopeIDsComputedOnlySweep(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	for i := range specs {
		s := &specs[i]
		a := s.attr("scope_ids")
		if a == nil || s.Endpoint.Immutable || s.Association != nil {
			continue
		}
		if a.Required || a.Optional || a.SendFromState || a.NoSend {
			continue
		}
		if scopeIDsRestampedOnUpdate[s.Name] {
			continue
		}
		t.Errorf("%s.scope_ids is computed-only on a mutable resource without sendFromState: "+
			"if the backend update replaces the record, every update wipes spec.scopeIds. "+
			"Add sendFromState, or record the backend evidence in scopeIDsRestampedOnUpdate", s.Name)
	}
}

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
// This test pins the flag on every spec listed in scopeIDsFullReplaceSpecs so it cannot be
// dropped again.
func TestScopeIDsSurviveFullDocumentUpdate(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	byName := map[string]*ResourceSpec{}
	for i := range specs {
		byName[specs[i].Name] = &specs[i]
	}

	for _, name := range scopeIDsFullReplaceSpecs {
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
