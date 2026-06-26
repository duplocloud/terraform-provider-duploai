# Look up a permission set by ID.
data "duploai_admin_permission_set" "example" {
  id = "<object-id>"
}

output "name" {
  value = data.duploai_admin_permission_set.example.name
}

output "allowed_workspaces" {
  value = data.duploai_admin_permission_set.example.allowed_workspaces
}
