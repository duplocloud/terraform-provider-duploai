# Look up an existing plan-level KMS key registration by its backend record ID.
data "duploai_plan_kms_key" "cmek" {
  workspace_id = "<workspace-id>"
  plan_id      = "<plan-id>"
  id           = "<key-entry-id>"
}

output "key_arn" {
  value = data.duploai_plan_kms_key.cmek.key_arn
}

output "key_id" {
  value = data.duploai_plan_kms_key.cmek.key_id
}
