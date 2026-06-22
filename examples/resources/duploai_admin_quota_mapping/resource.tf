# A quota definition to bind.
resource "duploai_admin_quota_definition" "monthly" {
  name      = "monthly-budget"
  type      = "Monthly"
  limit_usd = 2000
}

# Platform-wide cap (Platform + All, no target_ids).
resource "duploai_admin_quota_mapping" "platform_all" {
  name               = "platform-cap"
  quota_reference_id = duploai_admin_quota_definition.monthly.id
  scope              = "Platform"
  type               = "All"
}

# Per-workspace cap (Workspace + Workspace, target_ids required).
resource "duploai_admin_quota_mapping" "workspace_cap" {
  name               = "workspace-cap"
  quota_reference_id = duploai_admin_quota_definition.monthly.id
  scope              = "Workspace"
  type               = "Workspace"
  target_ids         = ["<workspace-id>"]
}
