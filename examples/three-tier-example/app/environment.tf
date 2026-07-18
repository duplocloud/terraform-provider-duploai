resource "duploai_environment" "this" {
  workspace_id = var.workspace_id
  name         = "${var.name_prefix}-env"
  scope_ids    = var.scope_ids
  plan_ids     = [duploai_plan.this.plan_id]
}
