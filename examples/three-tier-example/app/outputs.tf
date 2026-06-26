output "environment_id" { value = local.environment_id }
output "resource_group_id" { value = local.rg_id }
output "namespace_name" { value = local.ns }
# re-export so the services tier reads everything from app's state
output "workspace_id" { value = var.workspace_id }
output "scope_ids" { value = var.scope_ids }
output "region" { value = var.region }
output "name_prefix" { value = var.name_prefix }
