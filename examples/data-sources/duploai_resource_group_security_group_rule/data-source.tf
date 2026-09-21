# Look up an existing inbound rule by its AWS security group rule ID.
data "duploai_resource_group_security_group_rule" "existing" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  id                = "sgr-0123456789abcdef0"
}

output "rule_protocol" {
  value = data.duploai_resource_group_security_group_rule.existing.ip_protocol
}

output "rule_source" {
  value = coalesce(
    data.duploai_resource_group_security_group_rule.existing.cidr_ipv4,
    data.duploai_resource_group_security_group_rule.existing.cidr_ipv6,
    data.duploai_resource_group_security_group_rule.existing.referenced_group_id,
  )
}
