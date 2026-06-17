resource "duploai_network_baseline" "this" {
  workspace_id  = var.workspace_id
  name          = "${var.name_prefix}-network"
  description   = "Demo app network"
  scope_ids     = var.scope_ids
  region        = var.region
  cidr          = "10.40.0.0/16"
  az_count      = 2
  subnet_prefix = 24
  nat_mode      = "SingleAz"
  enable_dns    = true

  timeouts {
    create = "30m"
    delete = "15m"
  }
}
