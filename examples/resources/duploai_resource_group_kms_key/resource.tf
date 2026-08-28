# Register an existing customer-managed KMS key against a resource group.
# The key's own key policy must already trust this account/role; registering
# it here does not modify the key's policy.
#
# Downstream resources that support customer-managed encryption reference
# the registered key by ARN or ID via their own kms_key_id attribute; the
# backend resolves it against this registry.
resource "duploai_resource_group_kms_key" "cmek" {
  workspace_id      = "<workspace-id>"
  resource_group_id = duploai_resource_group.basic.resource_group_id

  key_name = "customer-cmek"
  key_arn  = "<kms-key-arn>"
}
