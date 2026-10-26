# A plain-text parameter
resource "duploai_ssm_parameter" "db_host" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name        = "/myapp/dev/db-host"
  type        = "String"
  value       = "db.internal.example.com"
  description = "Database host for the dev environment"

  tags = [
    { key = "team", value = "platform" },
  ]
}

# A comma-separated list
resource "duploai_ssm_parameter" "allowed_origins" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name  = "/myapp/dev/allowed-origins"
  type  = "StringList"
  value = "https://app.example.com,https://admin.example.com"
}

# An encrypted parameter with a customer-managed KMS key
resource "duploai_ssm_parameter" "db_password" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name   = "/myapp/dev/db-password"
  type   = "SecureString"
  value  = "<secret-value>"
  key_id = "<kms-key-id>" # omit to use the AWS-managed alias/aws/ssm
}
