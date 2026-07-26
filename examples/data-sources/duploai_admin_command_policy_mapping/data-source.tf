# Look up a command policy mapping by ID.
data "duploai_admin_command_policy_mapping" "example" {
  id = "<command-policy-mapping-id>"
}

output "policy_id" {
  value = data.duploai_admin_command_policy_mapping.example.policy_id
}

output "level" {
  value = data.duploai_admin_command_policy_mapping.example.level
}

output "target_ids" {
  value = data.duploai_admin_command_policy_mapping.example.target_ids
}
