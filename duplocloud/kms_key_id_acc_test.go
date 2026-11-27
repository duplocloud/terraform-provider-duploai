package duplocloud

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// A blank kms_key_id is rejected by terraform plan itself (DUPLOAI-5637): the
// error names the attribute and says to omit it or use null, and no plan that
// would replace the database is produced. Omitting the attribute plans
// cleanly, so null stays the way to say "use the platform default".
//
// Validation runs before any API call, so the IDs below need not exist; the
// provider still needs DUPLO_HOST / DUPLO_TOKEN to configure.

var kmsBlankMessage = regexp.MustCompile(`kms_key_id must not be empty; omit it or set it to null`)

func testAccKmsRdsConfig(resourceType, kms string) string {
	extra := ""
	if resourceType == "duploai_rds_instance" {
		extra = `db_instance_class = "db.t3.medium"`
	}
	kmsLine := ""
	if kms != "<omit>" {
		kmsLine = fmt.Sprintf("kms_key_id = %q", kms)
	}
	return fmt.Sprintf(`
resource %q "test" {
  workspace_id      = "000000000000000000000001"
  environment_id    = "000000000000000000000002"
  resource_group_id = "000000000000000000000003"
  scope_ids         = ["000000000000000000000004"]
  name              = "acc-kms-blank"
  engine            = "postgres"
  %s
  %s
}
`, resourceType, extra, kmsLine)
}

func TestAccKmsKeyID_BlankRejectedAtPlan(t *testing.T) {
	for _, rt := range []string{"duploai_rds_cluster", "duploai_rds_instance"} {
		t.Run(rt, func(t *testing.T) {
			steps := []resource.TestStep{}
			for _, blank := range []string{"", " ", "\t\n"} {
				steps = append(steps, resource.TestStep{
					Config:      testAccKmsRdsConfig(rt, blank),
					PlanOnly:    true,
					ExpectError: kmsBlankMessage,
				})
			}
			// Omitted: no validation error; the plan is a create.
			steps = append(steps, resource.TestStep{
				Config:             testAccKmsRdsConfig(rt, "<omit>"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			})
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps:                    steps,
			})
		})
	}
}
