# Register an existing customer-managed KMS key against a plan, so every
# resource group in the environments that plan is attached to can encrypt with
# it. Register at the plan level for one key serving a whole environment; use
# duploai_resource_group_kms_key when the key belongs to a single group.
#
# The key's own key policy must already trust this account/role — registering it
# here does not modify the policy.
#
# All three of key_name, key_id and key_arn are required by the API, and each
# must be unique within the plan.
resource "duploai_plan_kms_key" "cmek" {
  workspace_id = "<workspace-id>"
  plan_id      = duploai_plan.basic.plan_id

  key_name = "environment-cmek"
  key_id   = "1234abcd-12ab-34cd-56ef-1234567890ab"
  key_arn  = "arn:aws:kms:us-west-2:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
}

# Consuming resources resolve either form against the registry. Referencing the
# ARN keeps the dependency explicit, so Terraform registers the key first.
resource "duploai_node_group" "encrypted" {
  workspace_id      = "<workspace-id>"
  name              = "encrypted-nodes"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  scope_ids         = ["<scope-id>"]

  instance_types = ["t3.large"]
  ami_type       = "AL2023_x86_64_STANDARD"
  min_size       = 1
  max_size       = 2
  desired_size   = 1

  kms_key_id = duploai_plan_kms_key.cmek.key_arn
}
