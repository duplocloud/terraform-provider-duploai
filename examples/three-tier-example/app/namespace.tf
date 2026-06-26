resource "duploai_k8s_namespace" "this" {
  workspace_id      = var.workspace_id
  name              = "${var.name_prefix}-ns"
  environment_id    = local.environment_id
  resource_group_id = local.rg_id
  failure_retries   = 30
}
