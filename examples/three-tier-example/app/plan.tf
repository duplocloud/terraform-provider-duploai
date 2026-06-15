resource "duploai_plan" "this" {
  workspace_id        = var.workspace_id
  name                = "${var.name_prefix}-plan"
  scope_ids           = var.scope_ids
  region              = var.region
  network_baseline_id = local.network_id
}
