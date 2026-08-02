# Look up an Azure Key Vault by ID.
data "duploai_azure_key_vault" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_azure_key_vault.example.status
}

# Data-plane URI clients use to read secrets.
output "vault_uri" {
  value = data.duploai_azure_key_vault.example.vault_uri
}

output "key_vault_id" {
  value = data.duploai_azure_key_vault.example.key_vault_id
}

output "provisioning_state" {
  value = data.duploai_azure_key_vault.example.provisioning_state
}
