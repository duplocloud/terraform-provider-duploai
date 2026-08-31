# Import an existing KMS key registration.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - RESOURCE_GROUP_ID is the resource group's backend record ID (e.g. 6a2258e94703bc957a1b824e)
#  - KEY_ENTRY_ID is this registration's own backend record ID, not the AWS KMS key ID (e.g. 7b3369fa5814cd068b2c935f)
terraform import duploai_resource_group_kms_key.cmek WORKSPACE_ID/RESOURCE_GROUP_ID/KEY_ENTRY_ID
# Example:
# terraform import duploai_resource_group_kms_key.cmek 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e/7b3369fa5814cd068b2c935f
