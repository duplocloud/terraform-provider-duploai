# A plaintext AWS Secrets Manager secret
resource "duploai_aws_secret" "db_password" {
  workspace_id      = "<workspace-id>"
  name              = "db-password"
  resource_group_id = "<resource-group-id>"
  environment_id    = "<environment-id>"

  secret_value_type = "PlainText"
  secret_string     = "<secret-value>"
}

# A JSON secret with a customer-managed KMS key
resource "duploai_aws_secret" "app_config" {
  workspace_id      = "<workspace-id>"
  name              = "app-config"
  resource_group_id = "<resource-group-id>"
  environment_id    = "<environment-id>"

  secret_value_type  = "Json"
  secret_string      = jsonencode({ username = "admin", password = "<password>" })
  secret_description = "Application configuration secret"
  kms_key_id         = "<kms-key-id>"

  timeouts {
    create = "15m"
    delete = "10m"
  }
}

# Terraform 1.11 and later: the write-only argument keeps the secret out of the
# plan and state. Change the _wo_version number whenever the secret changes —
# that is what sends the new value. On older Terraform, use the attribute above.
resource "duploai_aws_secret" "write_only" {
  workspace_id      = "<workspace-id>"
  name              = "db-password-wo"
  resource_group_id = "<resource-group-id>"
  environment_id    = "<environment-id>"

  secret_value_type        = "PlainText"
  secret_string_wo         = var.db_password
  secret_string_wo_version = 1
}

variable "db_password" {
  type      = string
  sensitive = true
}
