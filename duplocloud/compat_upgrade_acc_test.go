package duplocloud

// Live upgrade acceptance (local only, `make testacc`): a resource created by
// the released v0.6.7 provider must plan NO changes when the current build
// takes over the same configuration and state. This is the runtime half of
// the backward-compatibility gate in compat_schema_test.go (which only proves
// the schemas are compatible): it also catches read-path, state-upgrade,
// identity and plan-modifier regressions that turn into a diff on upgrade.
//
// Gating follows acc_test.go: TF_ACC=1 plus DUPLO_HOST/DUPLO_TOKEN; the
// workspace-placed cases also need DUPLO_TEST_WORKSPACE_ID,
// DUPLO_TEST_ENVIRONMENT_ID, DUPLO_TEST_K8S_RESOURCE_GROUP_ID and
// DUPLO_TEST_K8S_NAMESPACE, and skip when they are unset. Step 1 downloads
// duplocloud/duploai 0.6.7 from the registry. Every object is named
// qa-compat-<rand> and is destroyed by the test.
//
// Run one: TF_ACC=1 go test ./duplocloud -run 'TestAccCompatUpgrade_v067/sqs' -v -timeout 60m

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// k8sLabelsReleasedDrift: v0.6.7 reads labels from a path the backend no
// longer fills (they now come back under result.k8sResource), so every v0.6.7
// refresh drops the declared labels and plans to re-add them. Live 2026-10-28.
const k8sLabelsReleasedDrift = "v0.6.7 refresh drops declared labels (labels now under result.k8sResource)"

type compatPlacement struct{ ws, env, rg, ns string }

type compatUpgradeCase struct {
	name      string
	placement bool // needs the DUPLO_TEST_* workspace placement
	// releasedDrift: v0.6.7 itself does not converge against the current
	// backend (a pre-existing released defect the current build fixes), so its
	// own post-apply refresh plan is not empty. Step 2 stays strict.
	releasedDrift string
	config        func(sfx string, p compatPlacement) string
}

func compatPlacementHeader(p compatPlacement) string {
	return fmt.Sprintf(`
  workspace_id      = %q
  environment_id    = %q
  resource_group_id = %q`, p.ws, p.env, p.rg)
}

var compatUpgradeCases = []compatUpgradeCase{
	{name: "admin_quota_definition", config: func(sfx string, _ compatPlacement) string {
		return fmt.Sprintf(`
resource "duploai_admin_quota_definition" "t" {
  name        = "qa-compat-quota-%s"
  type        = "Daily"
  limit_usd   = 1
  description = "qa-compat upgrade test"
}`, sfx)
	}},
	{name: "admin_skill_persona", config: func(sfx string, _ compatPlacement) string {
		return fmt.Sprintf(`
resource "duploai_admin_skill" "t" {
  name        = "qa-compat-skill-%[1]s"
  type        = "Custom"
  format      = "SkillMd"
  skill_md    = "# qa-compat skill\nNo-op skill for provider upgrade QA."
  description = "qa-compat upgrade test"
}
resource "duploai_admin_persona" "t" {
  name        = "qa-compat-persona-%[1]s"
  skill_ids   = [duploai_admin_skill.t.id]
  description = "qa-compat upgrade test"
}`, sfx)
	}},
	{name: "admin_command_policy", config: func(sfx string, _ compatPlacement) string {
		return fmt.Sprintf(`
resource "duploai_admin_command_policy" "t" {
  name        = "qa-compat-cpol-%s"
  block_list  = ["qa-compat-never-run-this"]
  description = "qa-compat upgrade test"
}`, sfx)
	}},
	{name: "admin_provider", config: func(sfx string, _ compatPlacement) string {
		return fmt.Sprintf(`
resource "duploai_admin_provider" "t" {
  name        = "qa-compat-prov-%s"
  type        = "other"
  category    = "other"
  description = "qa-compat upgrade test"
  credentials = [
    {
      name = "default"
      credential_fields = [
        { key = "apiKey", value = "qa-compat-fake-not-a-secret", type = "secret", is_sensitive = true },
      ]
    }
  ]
}`, sfx)
	}},
	// A permission set only grants on the workspace it names; this one grants
	// read on the designated test workspace and is destroyed afterwards.
	{name: "admin_permission_set", placement: true, config: func(sfx string, p compatPlacement) string {
		return fmt.Sprintf(`
resource "duploai_admin_permission_set" "t" {
  name        = "qa-compat-pset-%s"
  description = "qa-compat upgrade test"
  allowed_workspaces = [
    {
      workspace_id   = %q
      allowed_scopes = ["read:.*"]
    }
  ]
}`, sfx, p.ws)
	}},
	{name: "k8s_config_map", placement: true, releasedDrift: k8sLabelsReleasedDrift, config: func(sfx string, p compatPlacement) string {
		return fmt.Sprintf(`
resource "duploai_k8s_config_map" "t" {%s
  namespace_name    = %q
  name              = "qa-compat-cm-%s"
  data              = { K1 = "v1" }
  labels            = { qa = "compat" }
}`, compatPlacementHeader(p), p.ns, sfx)
	}},
	{name: "k8s_secret", placement: true, releasedDrift: k8sLabelsReleasedDrift, config: func(sfx string, p compatPlacement) string {
		return fmt.Sprintf(`
resource "duploai_k8s_secret" "t" {%s
  namespace_name    = %q
  name              = "qa-compat-sec-%s"
  type              = "Opaque"
  string_data       = { K1 = "qa-compat-dummy-value" }
  labels            = { qa = "compat" }
}`, compatPlacementHeader(p), p.ns, sfx)
	}},
	{name: "k8s_namespace", placement: true, config: func(sfx string, p compatPlacement) string {
		return fmt.Sprintf(`
resource "duploai_k8s_namespace" "t" {%s
  name              = "qa-compat-ns-%s"
  delete_protection = false
}`, compatPlacementHeader(p), sfx)
	}},
	{name: "sqs", placement: true, config: func(sfx string, p compatPlacement) string {
		return fmt.Sprintf(`
resource "duploai_sqs" "t" {%s
  name                       = "qa-compat-sqs-%s"
  queue_type                 = "Standard"
  visibility_timeout_seconds = 30
}`, compatPlacementHeader(p), sfx)
	}},
	{name: "sns", placement: true, config: func(sfx string, p compatPlacement) string {
		return fmt.Sprintf(`
resource "duploai_sns" "t" {%s
  name         = "qa-compat-sns-%s"
  display_name = "qa compat"
}`, compatPlacementHeader(p), sfx)
	}},
	{name: "ssm_parameter", placement: true, config: func(sfx string, p compatPlacement) string {
		return fmt.Sprintf(`
resource "duploai_ssm_parameter" "t" {%s
  name        = "/qa-compat/ssm-%s"
  type        = "String"
  value       = "qa-compat-value"
  description = "qa-compat upgrade test"
}`, compatPlacementHeader(p), sfx)
	}},
}

// TestAccCompatUpgrade_v067: create with registry v0.6.7, then the current
// build must refresh the same state and config to an empty plan.
func TestAccCompatUpgrade_v067(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test: set TF_ACC=1 (make testacc)")
	}
	// The current build is served under the same registry address as the
	// released one, so state written by v0.6.7 is adopted, not orphaned.
	t.Setenv("TF_ACC_PROVIDER_NAMESPACE", "duplocloud")

	p := compatPlacement{os.Getenv("DUPLO_TEST_WORKSPACE_ID"), os.Getenv("DUPLO_TEST_ENVIRONMENT_ID"),
		os.Getenv("DUPLO_TEST_K8S_RESOURCE_GROUP_ID"), os.Getenv("DUPLO_TEST_K8S_NAMESPACE")}
	only := os.Getenv("COMPAT_UPGRADE_ONLY") // optional comma list of case names

	for _, tc := range compatUpgradeCases {
		t.Run(tc.name, func(t *testing.T) {
			if only != "" && !strings.Contains(","+only+",", ","+tc.name+",") {
				t.Skip("not in COMPAT_UPGRADE_ONLY")
			}
			if tc.placement && (p.ws == "" || p.env == "" || p.rg == "" || p.ns == "") {
				t.Skip("set DUPLO_TEST_WORKSPACE_ID, DUPLO_TEST_ENVIRONMENT_ID, DUPLO_TEST_K8S_RESOURCE_GROUP_ID and DUPLO_TEST_K8S_NAMESPACE to run")
			}
			cfg := tc.config(strings.ToLower(acctest.RandString(6)), p)
			resource.Test(t, resource.TestCase{
				PreCheck: func() { testAccPreCheck(t) },
				Steps: []resource.TestStep{
					{
						ExternalProviders: map[string]resource.ExternalProvider{
							"duploai": {Source: "duplocloud/duploai", VersionConstraint: "0.6.7"},
						},
						Config:             cfg,
						ExpectNonEmptyPlan: tc.releasedDrift != "",
					},
					{
						ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
						Config:                   cfg,
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
					},
				},
			})
		})
	}
}
