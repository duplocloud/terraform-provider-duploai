# Permission set granting scoped access to a single workspace.
resource "duploai_admin_permission_set" "basic" {
  name = "read-only"

  allowed_workspaces = [
    {
      workspace_id   = "<workspace-id>"
      allowed_scopes = ["read:.*"]
    }
  ]
}

# Permission set spanning multiple workspaces, with allow/deny on scopes and agents.
resource "duploai_admin_permission_set" "full" {
  name        = "devops-access"
  description = "DevOps access across two workspaces"

  allowed_workspaces = [
    {
      workspace_id   = "<workspace-id-1>"
      allowed_scopes = ["aws:.*", "k8s:.*"]
      denied_scopes  = ["aws:iam:.*"]
      allowed_agents = ["<agent-id>"]
    },
    {
      workspace_id  = "<workspace-id-2>"
      denied_agents = ["<restricted-agent-id>"]
    }
  ]
}
