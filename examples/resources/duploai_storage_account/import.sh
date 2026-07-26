# Import an existing Azure storage account.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - STORAGE_ACCOUNT_ID is the ID of the storage account (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_storage_account.mysa WORKSPACE_ID/STORAGE_ACCOUNT_ID
# Example:
# terraform import duploai_storage_account.mysa 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
