# Attach an existing scope to an existing workspace.
resource "duploai_admin_workspace_scope_mapping" "example" {
  workspace_id = duploai_admin_workspace.example.id
  scope_id     = duploai_admin_scope.example.id
}

# Manage a scope with EITHER this resource OR the workspace's scope_ids list —
# never both, or the two will fight over the same link on every apply.
