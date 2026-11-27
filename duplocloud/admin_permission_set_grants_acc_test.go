package duplocloud

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// Live acceptance test for resource_grants on duploai_admin_permission_set
// (local only; needs DUPLO_HOST, DUPLO_TOKEN, DUPLO_TEST_WORKSPACE_ID and
// DUPLO_TEST_ENVIRONMENT_ID, skipped when the last two are unset).
// It pins what the unit stubs cannot: the real backend accepts the rules, a
// refresh after apply finds nothing to change whichever shape is configured,
// and moving from the legacy list to grants is an in-place update.

func testAccPermissionSetLegacy(name, ws string) string {
	return fmt.Sprintf(`
resource "duploai_admin_permission_set" "test" {
  name = %[1]q
  allowed_workspaces = [{ workspace_id = %[2]q }]
}
`, name, ws)
}

func testAccPermissionSetGrants(name, ws, env, verbs string) string {
	return fmt.Sprintf(`
resource "duploai_admin_permission_set" "test" {
  name = %[1]q
  resource_grants = {
    allow = [{
      path = [
        { node_type = "Workspace",   node_id = %[2]q },
        { node_type = "Environment", node_id = %[3]q },
      ]
      verbs              = %[4]s
      allow_all_children = true
    }]
  }
}
`, name, ws, env, verbs)
}

func TestAccAdminPermissionSet_resourceGrants(t *testing.T) {
	ws, env := os.Getenv("DUPLO_TEST_WORKSPACE_ID"), os.Getenv("DUPLO_TEST_ENVIRONMENT_ID")
	if os.Getenv("TF_ACC") != "" && (ws == "" || env == "") {
		t.Skip("DUPLO_TEST_WORKSPACE_ID and DUPLO_TEST_ENVIRONMENT_ID are required")
	}
	name := acctest.RandomWithPrefix("tfacc-ps")
	addr := "duploai_admin_permission_set.test"
	empty := resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyed("duploai_admin_permission_set", "/v1/aiservicedesk/admin/data/PermissionSet"),
		Steps: []resource.TestStep{
			// Legacy shape: still works, no drift.
			{Config: testAccPermissionSetLegacy(name, ws), ConfigPlanChecks: empty},
			// Move to grants: the legacy attribute leaves the config; an in-place
			// update, then nothing to change.
			{
				Config: testAccPermissionSetGrants(name, ws, env, `["GET"]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr(addr, "resource_grants.allow.0.verbs.#", "1"),
			},
			// Change a rule in place.
			{
				Config: testAccPermissionSetGrants(name, ws, env, `["GET", "PUT"]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr(addr, "resource_grants.allow.0.verbs.#", "2"),
			},
			// Import reproduces the same state.
			{ResourceName: addr, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
		},
	})
}
