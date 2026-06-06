# Basic EKS cluster baseline — linked to an existing network baseline (Standard mode).
# region, vpc_id, subnet_ids, and scope_ids are inherited from the network.
resource "duploai_cluster_baseline" "basic" {
  workspace_id = "<workspace-id>"
  name         = "prod-cluster"
  network_id   = "<network-baseline-id>"
  eks_version  = "1.34"
}

# Cluster referencing a managed network baseline, with a system node group and
# private API endpoint. The network is created first; the cluster inherits its
# VPC, subnets, region, and scope.
resource "duploai_network_baseline" "this" {
  workspace_id  = "<workspace-id>"
  name          = "prod-network"
  scope_ids     = ["<scope-id>"]
  region        = "us-east-1"
  cidr          = "10.0.0.0/16"
  az_count      = 2
  subnet_prefix = 24
}

resource "duploai_cluster_baseline" "full" {
  workspace_id = "<workspace-id>"
  name         = "prod-cluster-full"
  network_id   = duploai_network_baseline.this.network_id

  eks_version           = "1.34"
  cluster_type          = "Standard"
  api_server_visibility = "PublicAndPrivate"
  control_plane_logging = ["api", "audit", "authenticator"]
  cluster_ip_cidr       = "172.20.0.0/16"

  # Provision a default managed system node group alongside the cluster.
  system_node_group = {
    instance_type = "t3.large"
    min_size      = 2
    max_size      = 5
  }

  timeouts {
    create = "30m"
    update = "30m"
    delete = "15m"
  }
}
