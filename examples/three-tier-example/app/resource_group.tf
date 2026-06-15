# Tenant for the application (assignment §1)
resource "duploai_resource_group" "this" {
  workspace_id   = var.workspace_id
  name           = "${var.name_prefix}-ass30"
  description    = "Demo app tenant"
  scope_ids      = var.scope_ids
  region         = var.region
  environment_id = local.environment_id
  cluster_id     = local.cluster_id
  network_id     = local.network_id
}
