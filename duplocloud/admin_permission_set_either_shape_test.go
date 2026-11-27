package duplocloud

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Setting only one of allowed_workspaces / resource_grants must never show the
// other as a diff, whichever shape the backend returns.

func psLegacyShape() []any {
	return []any{map[string]any{
		"workspaceId": "ws-1", "allowedScopes": []any{"read:.*"}, "deniedScopes": []any{},
		"allowedAgents": []any{}, "deniedAgents": []any{},
	}}
}

// grants-only config, backend also returns a legacy list: no drift.
func TestPSEither_GrantsOnlyConfigBackendAlsoReturnsLegacy(t *testing.T) {
	_, srv := newPSStub(t, func(m map[string]any) map[string]any {
		m["allowedWorkspaces"] = psLegacyShape()
		return m
	})
	psUnit(t, srv.URL, resource.TestStep{Config: psGrantsCfg(srv.URL, `["GET"]`, `["PUT"]`)})
}

// grants-only config, backend omits the legacy list entirely: no drift.
func TestPSEither_GrantsOnlyConfigBackendOmitsLegacy(t *testing.T) {
	_, srv := newPSStub(t, func(m map[string]any) map[string]any {
		delete(m, "allowedWorkspaces")
		return m
	})
	psUnit(t, srv.URL, resource.TestStep{Config: psGrantsCfg(srv.URL, `["GET"]`, `["PUT"]`)})
}

// legacy-only config, backend returns grants too: no drift (already covered
// once; here with a second refresh step to prove it is stable).
func TestPSEither_LegacyOnlyThenRefreshStable(t *testing.T) {
	_, srv := newPSStub(t, func(m map[string]any) map[string]any {
		m["resourceGrants"] = map[string]any{
			"allow": []any{map[string]any{
				"path":  []any{map[string]any{"nodeType": "Workspace", "nodeId": "ws-1", "name": "WS One"}},
				"verbs": []any{"GET"}, "actions": []any{}, "scopes": []any{"read:.*"}, "allowAllChildren": true,
			}},
			"deny": []any{},
		}
		return m
	})
	cfg := psProvider(srv.URL) + `
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  allowed_workspaces = [{ workspace_id = "ws-1", allowed_scopes = ["read:.*"] }]
}`
	psUnit(t, srv.URL, resource.TestStep{Config: cfg}, resource.TestStep{Config: cfg, PlanOnly: true})
}

// migrate in place: legacy-only, then grants-only (legacy attribute removed
// from config). The second step must apply cleanly and then plan empty.
func TestPSEither_MigrateLegacyToGrantsConverges(t *testing.T) {
	_, srv := newPSStub(t, nil)
	legacy := psProvider(srv.URL) + `
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  allowed_workspaces = [{ workspace_id = "ws-1", allowed_scopes = ["read:.*"] }]
}`
	psUnit(t, srv.URL,
		resource.TestStep{Config: legacy},
		resource.TestStep{Config: psGrantsCfg(srv.URL, `["GET"]`, `["PUT"]`)},
		resource.TestStep{Config: psGrantsCfg(srv.URL, `["GET"]`, `["PUT"]`), PlanOnly: true},
	)
}

// and back: grants-only, then legacy-only.
func TestPSEither_GrantsToLegacyConverges(t *testing.T) {
	_, srv := newPSStub(t, nil)
	legacy := psProvider(srv.URL) + `
resource "duploai_admin_permission_set" "t" {
  name = "ps"
  allowed_workspaces = [{ workspace_id = "ws-1", allowed_scopes = ["read:.*"] }]
}`
	psUnit(t, srv.URL,
		resource.TestStep{Config: psGrantsCfg(srv.URL, `["GET"]`, `["PUT"]`)},
		resource.TestStep{Config: legacy},
		resource.TestStep{Config: legacy, PlanOnly: true},
	)
}
