# Public access, PostgreSQL authentication only.
resource "duploai_azure_postgres_flexible_server" "example" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name                   = "my-postgres-server"
  postgres_version       = "16"
  administrator_login    = "pgadmin"
  administrator_password = var.postgres_admin_password

  sku_tier = "Burstable"
  sku_name = "Standard_B1ms"

  storage_size_gb   = 32
  storage_auto_grow = "Enabled"

  backup_retention_days = 7

  firewall_rules = [
    {
      name             = "office"
      start_ip_address = "203.0.113.0"
      end_ip_address   = "203.0.113.255"
    },
  ]

  tags = {
    team = "platform"
  }
}

variable "postgres_admin_password" {
  type      = string
  sensitive = true
}

# Private access (VNet integration) with Microsoft Entra authentication alongside
# PostgreSQL authentication. delegated_subnet_resource_id is required once
# public_network_access is Disabled, and firewall rules are rejected in that mode.
resource "duploai_azure_postgres_flexible_server" "private" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name                   = "my-private-postgres"
  administrator_login    = "pgadmin"
  administrator_password = var.postgres_admin_password

  sku_tier = "GeneralPurpose"
  sku_name = "Standard_D2ds_v4"

  storage_size_gb        = 128
  high_availability_mode = "ZoneRedundant"
  backup_retention_days  = 14
  geo_redundant_backup   = "Enabled"

  password_auth         = "Enabled"
  active_directory_auth = "Enabled"

  aad_administrators = [
    {
      object_id      = "00000000-0000-0000-0000-000000000000"
      principal_name = "dba-team"
      principal_type = "Group"
    },
  ]

  public_network_access        = "Disabled"
  delegated_subnet_resource_id = "/subscriptions/<sub>/resourceGroups/<rg>/providers/Microsoft.Network/virtualNetworks/<vnet>/subnets/<subnet>"

  maintenance_window = {
    custom_window = "Enabled"
    day_of_week   = 0
    start_hour    = 2
    start_minute  = 0
  }
}
