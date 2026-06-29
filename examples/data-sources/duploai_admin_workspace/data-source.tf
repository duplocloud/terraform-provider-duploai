# Look up a workspace by ID.
data "duploai_admin_workspace" "example" {
  id = "<workspace-id>"
}

output "name" {
  value = data.duploai_admin_workspace.example.name
}

output "short_name" {
  value = data.duploai_admin_workspace.example.short_name
}

output "persona_ids" {
  value = data.duploai_admin_workspace.example.persona_ids
}

output "scope_ids" {
  value = data.duploai_admin_workspace.example.scope_ids
}
