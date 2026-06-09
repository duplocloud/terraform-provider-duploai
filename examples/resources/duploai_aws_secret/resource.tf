# A plaintext AWS Secrets Manager secret
resource "duploai_aws_secret" "db_password" {
  workspace_id      = "<workspace-id>"
  name              = "db-password"
  resource_group_id = "<resource-group-id>"

  secret_value_type = "PlainText"
  secret_string     = "<secret-value>"
}

# A JSON secret with a customer-managed KMS key
resource "duploai_aws_secret" "app_config" {
  workspace_id      = "<workspace-id>"
  name              = "app-config"
  resource_group_id = "<resource-group-id>"

  secret_value_type  = "Json"
  secret_string      = jsonencode({ username = "admin", password = "<password>" })
  secret_description = "Application configuration secret"
  kms_key_id         = "<kms-key-id>"

  timeouts {
    create = "15m"
    delete = "10m"
  }
}
