# Inbound rules on a network baseline's primary security group. Network-scoped
# sibling of duploai_resource_group_security_group_rule: same fields, one parent
# id instead of two, since a network is workspace-scoped.
#
# Each resource is ONE rule with ONE source, so declare a block per rule.

# 1) An IPv4 CIDR on a TCP port range.
resource "duploai_network_baseline_security_group_rule" "https_from_office" {
  workspace_id = "<workspace-id>"
  network_id   = duploai_network_baseline.main.network_id

  ip_protocol = "tcp"
  from_port   = 443
  to_port     = 443
  cidr_ipv4   = "203.0.113.0/24"
  description = "HTTPS from the office range"
}

# 2) An IPv6 CIDR.
resource "duploai_network_baseline_security_group_rule" "https_ipv6" {
  workspace_id = "<workspace-id>"
  network_id   = duploai_network_baseline.main.network_id

  ip_protocol = "tcp"
  from_port   = 443
  to_port     = 443
  cidr_ipv6   = "2001:db8::/64"
  description = "HTTPS over IPv6"
}

# 3) A resource group as the source — the platform resolves that group's own
#    security group id. It must be in the same workspace as the network and
#    already have its security group provisioned.
resource "duploai_network_baseline_security_group_rule" "from_app_tier" {
  workspace_id = "<workspace-id>"
  network_id   = duploai_network_baseline.main.network_id

  ip_protocol              = "tcp"
  from_port                = 5432
  to_port                  = 5432
  source_resource_group_id = duploai_resource_group.app.resource_group_id
  description              = "Postgres from the app tier"
}

# 4) ICMP — ping. from_port/to_port carry the ICMP type and code, not ports;
#    omit them and the platform sends -1/-1, meaning all types and all codes.
resource "duploai_network_baseline_security_group_rule" "ping" {
  workspace_id = "<workspace-id>"
  network_id   = duploai_network_baseline.main.network_id

  ip_protocol = "icmp"
  cidr_ipv4   = "10.0.0.0/8"
  description = "Ping from inside the VPC"
}

# 5) Echo request only: ICMP type 8, code 0.
resource "duploai_network_baseline_security_group_rule" "echo_request" {
  workspace_id = "<workspace-id>"
  network_id   = duploai_network_baseline.main.network_id

  ip_protocol = "icmp"
  from_port   = 8
  to_port     = 0
  cidr_ipv4   = "10.0.0.0/8"
  description = "ICMP echo request"
}

# 6) All protocols from a trusted range. Ports must be omitted for -1: the API
#    ignores them and reports -1, which would diff on every plan.
resource "duploai_network_baseline_security_group_rule" "all_from_peer" {
  workspace_id = "<workspace-id>"
  network_id   = duploai_network_baseline.main.network_id

  ip_protocol = "-1"
  cidr_ipv4   = "10.1.0.0/16"
  description = "All traffic from the peered network"
}
