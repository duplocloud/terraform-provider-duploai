# Import an existing inbound rule on a network baseline's security group.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 00000000000000000000aaaa)
#  - NETWORK_ID is the network baseline's record ID (e.g. 00000000000000000000bbbb)
#  - RULE_ID is the AWS security group rule ID (e.g. sgr-0123456789abcdef0), not a DuploCloud record ID
terraform import duploai_network_baseline_security_group_rule.https_from_office WORKSPACE_ID/NETWORK_ID/RULE_ID
# Example:
# terraform import duploai_network_baseline_security_group_rule.https_from_office 00000000000000000000aaaa/00000000000000000000bbbb/sgr-0123456789abcdef0
