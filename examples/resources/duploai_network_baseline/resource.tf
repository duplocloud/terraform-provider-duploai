# Basic network baseline — Create mode, no NAT, no flow logs
resource "duploai_network_baseline" "basic" {
  workspace_id  = "<workspace-id>"
  name          = "prod-network"
  scope_ids     = ["<scope-id>"]
  region        = "us-east-1"
  cidr          = "10.0.0.0/16"
  az_count      = 2
  subnet_prefix = 24
}

# Network with NAT gateway and flow logs enabled
resource "duploai_network_baseline" "with_nat_and_logs" {
  workspace_id  = "<workspace-id>"
  name          = "prod-network-full"
  scope_ids     = ["<scope-id>"]
  region        = "us-east-1"
  cidr          = "10.1.0.0/16"
  az_count      = 2
  subnet_prefix = 24

  nat_mode                 = "SingleAz"
  enable_dns               = true
  enable_flow_logs         = true
  flow_logs_retention_days = 30

  timeouts {
    create = "45m"
    update = "30m"
    delete = "15m"
  }
}

# Import an existing VPC (mode = "Import") — adopts a VPC the platform did not
# provision instead of creating one. Set vpc_id to the existing VPC; cidr and the
# other VPC details are read from it, so cidr is omitted.
resource "duploai_network_baseline" "imported" {
  workspace_id  = "<workspace-id>"
  name          = "imported-network"
  mode          = "Import"
  scope_ids     = ["<scope-id>"]
  region        = "us-east-1"
  vpc_id        = "vpc-0a1b2c3d4e5f67890"
  az_count      = 2
  subnet_prefix = 24
}
