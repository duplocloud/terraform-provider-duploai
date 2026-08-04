# Import an existing Key Vault secret.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - KEY_VAULT_ID is the vault's backend record ID (e.g. 6a2258e94703bc957a1b824e)
#  - SECRET_NAME is the secret's name within the vault (e.g. db-password)
terraform import duploai_azure_key_vault_secret.db_password WORKSPACE_ID/KEY_VAULT_ID/SECRET_NAME
# Example:
# terraform import duploai_azure_key_vault_secret.db_password 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e/db-password
#
# The value is write-only — the API returns metadata only, so it lands empty
# after an import. Put it in config and apply to bring the two in step; that
# writes a new version.
