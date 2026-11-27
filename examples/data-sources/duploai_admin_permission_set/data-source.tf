# Look up a permission set by ID.
data "duploai_admin_permission_set" "example" {
  id = "<object-id>"
}

output "name" {
  value = data.duploai_admin_permission_set.example.name
}

# The allow and deny rule lists that decide what this permission set reaches.
output "allow_rules" {
  value = data.duploai_admin_permission_set.example.resource_grants.allow
}

output "deny_rules" {
  value = data.duploai_admin_permission_set.example.resource_grants.deny
}

# Deprecated legacy list. Still returned, but nothing reads it when deciding
# access -- read resource_grants instead.
output "allowed_workspaces" {
  value = data.duploai_admin_permission_set.example.allowed_workspaces
}
