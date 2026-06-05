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
