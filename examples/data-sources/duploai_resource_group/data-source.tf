# Look up a resource group by ID.
data "duploai_resource_group" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_resource_group.example.status
}

output "security_group_id" {
  value = data.duploai_resource_group.example.security_group_id
}

output "iam_role_arn" {
  value = data.duploai_resource_group.example.iam_role_arn
}
