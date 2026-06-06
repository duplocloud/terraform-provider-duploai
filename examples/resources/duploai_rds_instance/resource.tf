# Basic PostgreSQL instance
resource "duploai_rds_instance" "basic" {
  workspace_id      = "<workspace-id>"
  name              = "prod-pg"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<resource-group-id>"

  engine               = "postgres"
  engine_version       = "16.4"
  db_instance_class    = "db.t3.medium"
  allocated_storage    = 50
  db_name              = "appdb"
  master_username      = "dbadmin"
  master_user_password = "<password>"
}

# Multi-AZ MySQL instance with encryption, backups, and a parameter override
resource "duploai_rds_instance" "mysql" {
  workspace_id      = "<workspace-id>"
  name              = "orders-db"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<resource-group-id>"

  engine               = "mysql"
  engine_version       = "8.0.39"
  db_instance_class    = "db.r6g.large"
  allocated_storage    = 100
  storage_type         = "gp3"
  db_name              = "orders"
  master_username      = "dbadmin"
  master_user_password = "<password>"

  multi_az                = true
  storage_encrypted       = "<encryption-mode>"
  kms_key_id              = "<kms-key-id>"
  backup_retention_period = 14
  deletion_protection     = true

  vpc_security_group_ids = ["<sg-id>"]

  parameters = [
    {
      name  = "max_connections"
      value = "200"
    }
  ]

  timeouts {
    create = "60m"
    update = "60m"
    delete = "30m"
  }
}
