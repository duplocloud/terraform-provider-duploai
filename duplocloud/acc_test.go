package duplocloud

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// Acceptance tests run only with TF_ACC=1 (`make testacc`) against a live
// DuploAI host given by DUPLO_HOST / DUPLO_TOKEN (CI: the DUPLO_TEST_HOST /
// DUPLO_TEST_TOKEN secrets). Without TF_ACC, resource.Test skips them, so
// `go test ./...` stays a pure unit run.
//
// They pin the behaviour this provider has had to fix live: no drift after
// create, in-place updates that keep stable outputs known (a dependent is not
// replaced), import, clear import-ID errors and clean destroy.

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"duploai": providerserver.NewProtocol6WithError(New()),
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, k := range []string{"DUPLO_HOST", "DUPLO_TOKEN"} {
		if os.Getenv(k) == "" {
			t.Fatalf("%s must be set for acceptance tests", k)
		}
	}
}

// testAccCheckDestroyed verifies each resource of the given type is gone: a GET
// on its URL returns 404 (or the object reports DeProvisioned).
func testAccCheckDestroyed(resourceType, uriBase string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client, err := duplosdk.NewClient(os.Getenv("DUPLO_HOST"), os.Getenv("DUPLO_TOKEN"), false, 60*time.Second)
		if err != nil {
			return err
		}
		for _, rs := range s.RootModule().Resources {
			if rs.Type != resourceType {
				continue
			}
			id := rs.Primary.ID
			objID := id[strings.LastIndex(id, "/")+1:]
			api := duplosdk.NewRESTResource[map[string]any](client, duplosdk.Endpoint{UriBase: uriBase}, nil, nil)
			obj, cerr := api.Get(objID)
			if cerr != nil {
				if cerr.Status() == http.StatusNotFound {
					continue
				}
				return fmt.Errorf("checking %s %s was destroyed: %w", resourceType, id, cerr)
			}
			if obj != nil && (*obj)["status"] == "DeProvisioned" {
				continue
			}
			return fmt.Errorf("%s %s still exists after destroy", resourceType, id)
		}
		return nil
	}
}

func testAccQuotaConfig(name string, limit int, description string) string {
	return fmt.Sprintf(`
resource "duploai_admin_quota_definition" "test" {
  name        = %[1]q
  type        = "Daily"
  limit_usd   = %[2]d
  description = %[3]q
}

# Replaced if the quota's id or created_at goes "(known after apply)" during an
# in-place update: the RCA pattern (a dependent replaced by an unrelated change).
resource "terraform_data" "dependent" {
  triggers_replace = [duploai_admin_quota_definition.test.id, duploai_admin_quota_definition.test.created_at]
}
`, name, limit, description)
}

func TestAccAdminQuotaDefinition_lifecycle(t *testing.T) {
	name := acctest.RandomWithPrefix("tfacc-quota")
	addr := "duploai_admin_quota_definition.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// terraform_data is built into Terraform 1.4+.
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_4_0)},
		CheckDestroy:           testAccCheckDestroyed("duploai_admin_quota_definition", "/v1/aiservicedesk/admin/data/Quotas"),
		Steps: []resource.TestStep{
			// Create; a refresh right after must find nothing to change.
			{
				Config: testAccQuotaConfig(name, 10, "acceptance test"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(addr, "name", name),
					resource.TestCheckResourceAttr(addr, "limit_usd", "10"),
					resource.TestCheckResourceAttrSet(addr, "created_at"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			// In-place update: the quota updates, the dependent on its stable
			// outputs is untouched, and nothing drifts afterwards.
			{
				Config: testAccQuotaConfig(name, 20, "acceptance test updated"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("terraform_data.dependent", plancheck.ResourceActionNoop),
						plancheck.ExpectKnownValue(addr, tfjsonpath.New("created_at"), knownvalue.NotNull()),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr(addr, "limit_usd", "20"),
			},
			// Import reproduces the same state.
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			// A malformed import ID fails up front, before any API call, and names
			// the expected format. This resource's ID is the bare object id, so a
			// blank one is the malformed case (extra segments are ignored, as
			// parseID always has).
			{
				ResourceName:  addr,
				ImportState:   true,
				ImportStateId: " ",
				ExpectError:   regexp.MustCompile(`Invalid import ID`),
			},
		},
	})
}

// The k8s labels drift fix: platform-stamped labels never show as drift, and a
// declared label still round-trips. Needs a cluster, so it runs only when the
// placement IDs are provided.
func TestAccK8sConfigMap_labelsNoDrift(t *testing.T) {
	ws, env, rg, ns := os.Getenv("DUPLO_TEST_WORKSPACE_ID"), os.Getenv("DUPLO_TEST_ENVIRONMENT_ID"), os.Getenv("DUPLO_TEST_K8S_RESOURCE_GROUP_ID"), os.Getenv("DUPLO_TEST_K8S_NAMESPACE")
	if ws == "" || env == "" || rg == "" || ns == "" {
		t.Skip("set DUPLO_TEST_WORKSPACE_ID, DUPLO_TEST_ENVIRONMENT_ID, DUPLO_TEST_K8S_RESOURCE_GROUP_ID and DUPLO_TEST_K8S_NAMESPACE to run")
	}
	name := acctest.RandomWithPrefix("tfacc-cm")
	cfg := func(env2 string) string {
		return fmt.Sprintf(`
resource "duploai_k8s_config_map" "test" {
  workspace_id      = %[1]q
  environment_id    = %[2]q
  resource_group_id = %[3]q
  namespace_name    = %[4]q
  name              = %[5]q
  labels            = { app = "tfacc", env = %[6]q }
  data              = { key = "value" }
}
`, ws, env, rg, ns, name, env2)
	}
	addr := "duploai_k8s_config_map.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// The identity steps need Terraform 1.12+.
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_12_0)},
		Steps: []resource.TestStep{
			{
				Config: cfg("test"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: cfg("stage2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr(addr, "labels.env", "stage2"),
				// Identity {workspace_id, id} survives the in-place update.
				ConfigStateChecks: []statecheck.StateCheck{statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{
					"workspace_id": knownvalue.StringExact(ws), "id": knownvalue.NotNull(),
				})},
			},
			// No import step here: after import the labels filter keeps only keys
			// already in state (none yet) and scope_ids reads null, so the first
			// plan re-adds them. A known import-fidelity gap, separate from
			// identity; import by identity is covered by the quota test.
		},
	})
}

// Resource identity (Terraform 1.12+): set on create, unchanged through an
// in-place update (the framework itself fails with "Unexpected Identity
// Change" if it moves), and importable with an `import { identity = {...} }`
// block. The quota has no path parameters, so its identity is {id}.
func TestAccAdminQuotaDefinition_identity(t *testing.T) {
	name := acctest.RandomWithPrefix("tfacc-quota-id")
	addr := "duploai_admin_quota_definition.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_12_0)},
		CheckDestroy:             testAccCheckDestroyed("duploai_admin_quota_definition", "/v1/aiservicedesk/admin/data/Quotas"),
		Steps: []resource.TestStep{
			{
				Config:            testAccQuotaConfig(name, 10, "identity test"),
				ConfigStateChecks: []statecheck.StateCheck{statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.NotNull()})},
			},
			{
				Config: testAccQuotaConfig(name, 20, "identity test updated"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{statecheck.ExpectIdentity(addr, map[string]knownvalue.Check{"id": knownvalue.NotNull()})},
			},
			{
				ResourceName:    addr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
		},
	})
}
