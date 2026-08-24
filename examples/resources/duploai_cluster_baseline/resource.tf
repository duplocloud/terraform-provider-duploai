# Basic EKS cluster baseline — linked to an existing network baseline (Standard mode).
# region, vpc_id, subnet_ids, and scope_ids are inherited from the network.
resource "duploai_cluster_baseline" "basic" {
  workspace_id = "<workspace-id>"
  name         = "prod-cluster"
  network_id   = "<network-baseline-id>"
  version      = "1.34"
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

  version               = "1.34"
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

# ── Azure (AKS) variations ────────────────────────────────────────────────────
# All Azure clusters link to an Azure network baseline; the VNet, subnets, region,
# and scope are derived from it. Azure-specific settings go in the nested `azure`
# block.
#
# CONFLICTS — these are AWS (EKS) only and cannot be combined with the `azure`
# block (the provider errors at plan time):
#   - control_plane_logging   (AKS uses diagnostic settings, not this list)
#   - system_node_group       (EC2 node group; Azure uses azure.system_node_pool)
#   - cluster_type = "Auto"   (EKS Auto Mode; Azure supports only "Standard")
#   - vpc_id / subnet_ids     (AWS-only outputs; Azure exposes azure.* subnet IDs)
#
# SPECIFIC-CASE fields and their prerequisites on the linked network:
#   - azure.network_mode = "AzureCniPodSubnet"    → network must have an AksPods subnet
#   - azure.enable_agic = true                    → network must have an ApplicationGateway subnet
#   - azure.enable_workload_identity = true       → enables the AKS OIDC issuer; see azure_oidc_issuer_url
#   - domain_name_filter                          → the Azure public DNS zone(s) must already exist
#   - api_server_visibility = "Private"           → private API endpoint only
#   - cluster_ip_cidr                             → optional K8s service CIDR (AKS default when unset)

# Minimal Azure cluster — only the required inputs. network_mode defaults to
# AzureCniOverlay, AGIC is enabled, and the system node pool uses its defaults
# (Standard_DS2_v2, 2 nodes, autoscaling 2–5).
resource "duploai_cluster_baseline" "azure_minimal" {
  workspace_id = "<workspace-id>"
  name         = "aks-minimal"
  cloud        = "Azure"
  network_id   = "<azure-network-baseline-id>"
  version      = "1.35"
}

# Fully-customized Azure cluster — public+private API, a custom service CIDR, AGIC,
# a sized node pool, tags, and external-dns zones.
resource "duploai_cluster_baseline" "azure_full" {
  workspace_id = "<workspace-id>"
  name         = "aks-full"
  cloud        = "Azure"
  network_id   = "<azure-network-baseline-id>"

  version               = "1.35"
  api_server_visibility = "PublicAndPrivate"
  cluster_ip_cidr       = "10.2.0.0/24" # Kubernetes service CIDR (AKS default when unset)

  azure = {
    network_mode             = "AzureCniOverlay"
    enable_agic              = true
    enable_workload_identity = true

    system_node_pool = {
      vm_size             = "Standard_DS4_v2"
      count               = 3
      enable_auto_scaling = true
      min_count           = 3
      max_count           = 10
    }

    tags = {
      team        = "platform"
      environment = "production"
    }
  }

  # Requires these Azure public DNS zones to already exist in the subscription.
  domain_name_filter = "dev.example.com,apps.example.com"

  timeouts {
    create = "60m"
    update = "45m"
    delete = "30m"
  }
}

# Pod Subnet networking — assigns pod IPs from a dedicated subnet. Requires the
# linked network to have an AksPods subnet.
resource "duploai_cluster_baseline" "azure_pod_subnet" {
  workspace_id = "<workspace-id>"
  name         = "aks-podsubnet"
  cloud        = "Azure"
  network_id   = "<azure-network-baseline-id>"
  version      = "1.35"

  azure = {
    network_mode = "AzureCniPodSubnet"
    enable_agic  = true
  }
}

# Private cluster with no ingress controller — private API endpoint and AGIC off
# (no Application Gateway subnet needed on the network).
resource "duploai_cluster_baseline" "azure_private" {
  workspace_id          = "<workspace-id>"
  name                  = "aks-private"
  cloud                 = "Azure"
  network_id            = "<azure-network-baseline-id>"
  version               = "1.35"
  api_server_visibility = "Private"

  azure = {
    network_mode = "AzureCniOverlay"
    enable_agic  = false
  }
}

# Import an existing AKS cluster (mode = "Import"). `name` must match the existing
# cluster; version and Azure details are auto-discovered. Provide network_id (for
# the region/scope) or region directly.
resource "duploai_cluster_baseline" "azure_imported" {
  workspace_id = "<workspace-id>"
  mode         = "Import"
  cloud        = "Azure"
  name         = "existing-aks-cluster"
  network_id   = "<azure-network-baseline-id>"

  # version / region / VNet / subnets are auto-discovered — leave unset.
}

# On-premise / bare Kubernetes cluster (K8S_ONLY) — registers an existing cluster
# via its Kubernetes scope. No cloud network is provisioned, so network_id is
# omitted and scope_ids is set explicitly.
resource "duploai_cluster_baseline" "onprem" {
  workspace_id = "<workspace-id>"
  name         = "onprem-cluster"
  cloud        = "K8S_ONLY"
  scope_ids    = ["<k8s-scope-id>"]
  version      = "1.34"
}

# Import an existing installer-provisioned bare-Kubernetes cluster (K8S_ONLY,
# mode = Import) by its scope. No cloud network is provisioned, so network_id
# is omitted; scope_id (not scope_ids) identifies the cluster. Delete is a
# no-op for this combination (the cluster isn't deprovisioned by Terraform) —
# see endpoint.deprovision.skipWhen.
resource "duploai_cluster_baseline" "onprem_imported" {
  workspace_id = "<workspace-id>"
  name         = "installer-poc-vcfa-vks"
  cloud        = "K8S_ONLY"
  mode         = "Import"
  scope_id     = "<k8s-scope-id>"
}

# Import an existing EKS cluster (mode = "Import") — adopts a cluster the platform
# did not provision. `name` must match the existing cluster's name; the platform
# finds it by name + region + cloud and auto-discovers version, VPC, and subnets.
# Provide either network_id (which supplies the region) or region directly.
resource "duploai_cluster_baseline" "imported" {
  workspace_id = "<workspace-id>"
  mode         = "Import"
  cloud        = "Aws"
  name         = "existing-eks-cluster" # name of the EKS cluster to adopt
  network_id   = "<network-baseline-id>"
  scope_ids    = ["<scope-id>"]

  # version / region / vpc / subnets are auto-discovered — leave unset.
}
