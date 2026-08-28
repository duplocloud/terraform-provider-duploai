# Look up an existing KMS key registration by its backend record ID.
data "duploai_resource_group_kms_key" "cmek" {
  workspace_id      = "<workspace-id>"
  resource_group_id = "<resource-group-id>"
  id                = "<key-entry-id>"
}

output "key_arn" {
  value = data.duploai_resource_group_kms_key.cmek.key_arn
}

output "key_id" {
  value = data.duploai_resource_group_kms_key.cmek.key_id
}
