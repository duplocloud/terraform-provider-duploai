# Bind a command policy platform-wide (System level — no target_ids).
# Only one active System-level mapping may exist at a time.
resource "duploai_admin_command_policy_mapping" "system" {
  name      = "org-default"
  policy_id = "<command-policy-id>"
  level     = "System"
}

# Bind a command policy to specific workspaces (Workspace level).
# target_ids is required for Workspace/Project levels.
resource "duploai_admin_command_policy_mapping" "workspace" {
  name       = "platform-team-policy"
  policy_id  = "<command-policy-id>"
  level      = "Workspace"
  target_ids = ["<workspace-id-1>", "<workspace-id-2>"]
}

# Bind a command policy to specific projects (Project level).
resource "duploai_admin_command_policy_mapping" "project" {
  name       = "sensitive-project-policy"
  policy_id  = "<command-policy-id>"
  level      = "Project"
  target_ids = ["<project-id>"]
}
