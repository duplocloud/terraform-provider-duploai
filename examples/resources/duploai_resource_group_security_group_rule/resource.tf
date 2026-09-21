# Inbound rules on a resource group's primary security group. Each resource is
# ONE rule with ONE source, so declare a block per rule.

# 1) An IPv4 CIDR on a TCP port range.
resource "duploai_resource_group_security_group_rule" "https_from_office" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = duploai_resource_group.basic.resource_group_id

  ip_protocol = "tcp"
  from_port   = 443
  to_port     = 443
  cidr_ipv4   = "203.0.113.0/24"
  description = "HTTPS from the office range"
}

# 2) An IPv6 CIDR.
resource "duploai_resource_group_security_group_rule" "https_ipv6" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = duploai_resource_group.basic.resource_group_id

  ip_protocol = "tcp"
  from_port   = 443
  to_port     = 443
  cidr_ipv6   = "2001:db8::/64"
  description = "HTTPS over IPv6"
}

# 3) Another resource group as the source — the platform resolves that group's
#    own security group id, so no sg- value is hardcoded. The referenced group
#    must be in the same workspace and already provisioned.
resource "duploai_resource_group_security_group_rule" "from_app_tier" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = duploai_resource_group.db.resource_group_id

  ip_protocol              = "tcp"
  from_port                = 5432
  to_port                  = 5432
  source_resource_group_id = duploai_resource_group.app.resource_group_id
  description              = "Postgres from the app tier"
}

# 4) ICMP — ping. from_port/to_port are the ICMP type and code, not ports;
#    omit them and the platform sends -1/-1, meaning all types and all codes.
resource "duploai_resource_group_security_group_rule" "ping" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = duploai_resource_group.basic.resource_group_id

  ip_protocol = "icmp"
  cidr_ipv4   = "10.0.0.0/8"
  description = "Ping from inside the VPC"
}

# 5) Echo request only: ICMP type 8, code 0.
resource "duploai_resource_group_security_group_rule" "echo_request" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = duploai_resource_group.basic.resource_group_id

  ip_protocol = "icmp"
  from_port   = 8
  to_port     = 0
  cidr_ipv4   = "10.0.0.0/8"
  description = "ICMP echo request"
}

# 6) All protocols and ports from a trusted range. Ports are ignored for -1.
resource "duploai_resource_group_security_group_rule" "all_from_peer" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = duploai_resource_group.basic.resource_group_id

  ip_protocol = "-1"
  cidr_ipv4   = "10.1.0.0/16"
  description = "All traffic from the peered network"
}
