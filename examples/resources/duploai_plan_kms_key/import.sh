# Import an existing plan-level KMS key registration.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 00000000000000000000aaaa)
#  - PLAN_ID is the plan's backend record ID (e.g. 00000000000000000000bbbb)
#  - KEY_ENTRY_ID is this registration's own backend record ID, not the AWS KMS key ID (e.g. 000000000000000000000000000cccc1)
terraform import duploai_plan_kms_key.cmek WORKSPACE_ID/PLAN_ID/KEY_ENTRY_ID
# Example:
# terraform import duploai_plan_kms_key.cmek 00000000000000000000aaaa/00000000000000000000bbbb/000000000000000000000000000cccc1
