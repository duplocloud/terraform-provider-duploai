# Look up a Key Vault secret's METADATA. The value is not returned — Key Vault
# exposes it through a separate endpoint this data source does not call.
data "duploai_azure_key_vault_secret" "db_password" {
  workspace_id = "<workspace-id>"
  key_vault_id = "<key-vault-record-id>"
  id           = "db-password"
}

output "version" {
  value = data.duploai_azure_key_vault_secret.db_password.version
}

output "enabled" {
  value = data.duploai_azure_key_vault_secret.db_password.enabled
}

output "expires_on" {
  value = data.duploai_azure_key_vault_secret.db_password.expires_on
}

output "secret_id" {
  value = data.duploai_azure_key_vault_secret.db_password.secret_id
}
