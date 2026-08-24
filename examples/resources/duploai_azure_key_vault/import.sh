# Import an existing Azure Key Vault.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - VAULT_ID is the ID of the vault record (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_azure_key_vault.example WORKSPACE_ID/VAULT_ID
# Example:
# terraform import duploai_azure_key_vault.example 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
