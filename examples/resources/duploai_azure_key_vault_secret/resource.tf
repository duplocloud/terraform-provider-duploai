# A secret in a vault managed by the same configuration.
resource "duploai_azure_key_vault_secret" "db_password" {
  workspace_id = "<workspace-id>"

  # The vault's BACKEND record id, not its ARM resource id.
  key_vault_id = duploai_azure_key_vault.example.vault_record_id

  name  = "db-password"
  value = var.db_password

  content_type = "text/plain"

  tags = {
    app = "payments"
  }
}

variable "db_password" {
  type      = string
  sensitive = true
}

# With a validity window and disabled until it is needed.
resource "duploai_azure_key_vault_secret" "rotating_key" {
  workspace_id = "<workspace-id>"
  key_vault_id = duploai_azure_key_vault.example.vault_record_id

  name  = "api-key-next"
  value = var.next_api_key

  enabled      = false
  not_before   = "2027-01-01T00:00:00Z"
  expires_on   = "2027-04-01T00:00:00Z"
  content_type = "application/json"
}

variable "next_api_key" {
  type      = string
  sensitive = true
}
