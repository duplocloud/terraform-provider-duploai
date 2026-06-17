resource "duploai_rds_instance" "db" {
  workspace_id         = local.workspace_id
  name                 = "app"
  description          = "Demo app database (MySQL — DocumentDB not implemented)"
  scope_ids            = local.scope_ids
  resource_group_id    = local.resource_group_id
  environment_id       = local.environment_id
  allocated_storage    = 20
  engine               = "mysql"
  engine_version       = "8.0"
  db_instance_class    = "db.t3.medium"
  db_name              = "app"
  master_username      = "appadmin"
  master_user_password = var.db_master_password
}
