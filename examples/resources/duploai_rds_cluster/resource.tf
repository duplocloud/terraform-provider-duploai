# Basic Aurora PostgreSQL cluster with a managed master password
resource "duploai_rds_cluster" "basic" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  name              = "prod-db"
  scope_ids         = ["<scope-id>"]

  engine          = "aurora-postgresql"
  engine_version  = "16.4"
  db_name         = "appdb"
  master_username = "dbadmin"

  manage_master_user_password = true

  cluster_instances = [
    {
      name              = "writer"
      db_instance_class = "db.r6g.large"
    }
  ]
}

# Serverless v2 cluster with backups, encryption, and a read replica
resource "duploai_rds_cluster" "serverless" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  name              = "analytics-db"
  scope_ids         = ["<scope-id>"]

  engine          = "aurora-postgresql"
  engine_version  = "16.4"
  db_name         = "analytics"
  master_username = "dbadmin"

  manage_master_user_password = true
  storage_encrypted           = "<encryption-mode>"
  kms_key_id                  = "<kms-key-id>"

  backup_retention_period      = 14
  preferred_backup_window      = "07:00-09:00"
  preferred_maintenance_window = "sun:05:00-sun:06:00"
  deletion_protection          = true
  enable_performance_insights  = true

  enable_cloudwatch_logs_exports = ["postgresql"]

  serverless_v2_scaling = {
    min_capacity = 0.5
    max_capacity = 8
  }

  cluster_parameters = [
    {
      name  = "rds.force_ssl"
      value = "1"
    }
  ]

  cluster_instances = [
    {
      name              = "writer"
      db_instance_class = "db.serverless"
      promotion_tier    = 0
    },
    {
      name              = "reader"
      db_instance_class = "db.serverless"
      promotion_tier    = 1
    },
  ]

  timeouts {
    create = "90m"
    update = "60m"
    delete = "30m"
  }
}
