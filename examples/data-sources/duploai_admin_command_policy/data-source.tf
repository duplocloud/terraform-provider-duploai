# Look up a command policy by ID.
data "duploai_admin_command_policy" "example" {
  id = "<command-policy-id>"
}

output "name" {
  value = data.duploai_admin_command_policy.example.name
}

output "allow_list" {
  value = data.duploai_admin_command_policy.example.allow_list
}

output "block_list" {
  value = data.duploai_admin_command_policy.example.block_list
}
