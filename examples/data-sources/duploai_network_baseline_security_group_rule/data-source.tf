# Look up an existing inbound rule by its AWS security group rule ID.
data "duploai_network_baseline_security_group_rule" "existing" {
  workspace_id = "<workspace-id>"
  network_id   = "<network-id>"
  id           = "sgr-0123456789abcdef0"
}

output "rule_protocol" {
  value = data.duploai_network_baseline_security_group_rule.existing.ip_protocol
}

# Exactly one of these is populated, depending on the rule's source type.
output "rule_source" {
  value = coalesce(
    data.duploai_network_baseline_security_group_rule.existing.cidr_ipv4,
    data.duploai_network_baseline_security_group_rule.existing.cidr_ipv6,
    data.duploai_network_baseline_security_group_rule.existing.referenced_group_id,
  )
}
