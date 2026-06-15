resource "duploai_k8s_secret" "db" {
  workspace_id      = local.workspace_id
  name              = "db"
  scope_ids         = local.scope_ids
  resource_group_id = local.resource_group_id
  namespace_name    = local.namespace_name
  environment_id    = local.environment_id
  type              = "Opaque"
  string_data = {
    DB_ENGINE   = "mysql"
    DB_HOST     = "<db-endpoint>" # fill from duploai_rds_instance.db endpoint after provision
    DB_PORT     = "3306"
    DB_USER     = "appadmin"
    DB_PASSWORD = var.db_master_password
    DB_NAME     = "app"
  }

  depends_on = [duploai_rds_instance.db]
}
