# Import an existing inbound rule.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 00000000000000000000aaaa)
#  - ENVIRONMENT_ID is the environment's record ID (e.g. 00000000000000000000bbbb)
#  - RESOURCE_GROUP_ID is the resource group's record ID (e.g. 00000000000000000000cccc)
#  - RULE_ID is the AWS security group rule ID (e.g. sgr-0123456789abcdef0), not a DuploCloud record ID
terraform import duploai_resource_group_security_group_rule.https_from_office WORKSPACE_ID/ENVIRONMENT_ID/RESOURCE_GROUP_ID/RULE_ID
# Example:
# terraform import duploai_resource_group_security_group_rule.https_from_office 00000000000000000000aaaa/00000000000000000000bbbb/00000000000000000000cccc/sgr-0123456789abcdef0
