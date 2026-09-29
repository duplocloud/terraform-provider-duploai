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
#   - azure.addon_profiles                        → any other AKS add-on (keyvault-secrets,
#                                                   monitoring, azure-policy); an ingressApplicationGateway
#                                                   entry needs azure.enable_agic = false
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

    # Any other AKS add-on, keyed by its ARM add-on name. Merged as-is into the
    # managed cluster. For AGIC, enable_agic above is the simple path; see the
    # advanced/brownfield examples at the end of this file for the alternative.
    addon_profiles = {
      azureKeyvaultSecretsProvider = {
        enabled = true
        config = {
          enableSecretRotation = "true"
        }
      }
      azurepolicy = {
        enabled = true
      }
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

# helpdesk_vpc_peering on a cluster is read-only: the platform copies it from
# the linked network on every write and uses it to open inbound 443 for the
# helpdesk CIDR on the EKS control-plane security group. Change the setting on
# the network baseline (helpdesk_vpc_peering_enabled), not here.
output "cluster_helpdesk_peering_enabled" {
  value = duploai_cluster_baseline.basic.helpdesk_vpc_peering.enabled
}

output "cluster_helpdesk_allowed_cidrs" {
  value = duploai_cluster_baseline.basic.helpdesk_vpc_peering.helpdesk_vpc_cidr_blocks
}

# ─── Advanced AGIC: you own the add-on entry ────────────────────────────────────
# enable_agic = false is REQUIRED — it defaults to true, and the API rejects the
# bool and an ingressApplicationGateway entry being set together.

# (a) Greenfield with custom settings — AKS still creates the gateway, but you
#     choose its name, the subnet, and which namespaces AGIC watches.
resource "duploai_cluster_baseline" "azure_agic_custom" {
  workspace_id = "<workspace-id>"
  name         = "aks-agic-custom"
  cloud        = "Azure"
  network_id   = "<azure-network-baseline-id>"
  version      = "1.35"

  azure = {
    enable_agic = false

    addon_profiles = {
      ingressApplicationGateway = {
        enabled = true
        config = {
          subnetId               = "<ApplicationGateway subnet id of the linked network>"
          applicationGatewayName = "appgw-custom"
          watchNamespace         = "default,apps"
        }
      }
    }
  }
}

# (b) Brownfield — attach AGIC to an Application Gateway that already exists.
#     No subnet is resolved, so the gateway may live in any resource group or
#     VNet. Use this when the gateway needs settings AKS will not create for you
#     (for example properties.globalConfiguration).
resource "duploai_cluster_baseline" "azure_agic_brownfield" {
  workspace_id = "<workspace-id>"
  name         = "aks-agic-byo"
  cloud        = "Azure"
  network_id   = "<azure-network-baseline-id>"
  version      = "1.35"

  azure = {
    enable_agic = false

    addon_profiles = {
      ingressApplicationGateway = {
        enabled = true
        config = {
          # Mutually exclusive with subnetId.
          applicationGatewayId = "/subscriptions/<sub>/resourceGroups/<rg>/providers/Microsoft.Network/applicationGateways/<name>"
        }
      }
    }
  }
}

# ─── Application Gateway settings the cluster body cannot reach ────────────────
# Properties of the gateway itself — globalConfiguration (request/response
# buffering), WAF settings, autoscale — live on the
# Microsoft.Network/applicationGateways resource, NOT on the cluster. No key in
# addon_profiles reaches them: an add-on profile's config is a flat
# map[string]string on the AKS resource, and the gateway is a different resource.
#
# On the greenfield paths AKS creates the gateway, so there is nothing to
# configure at cluster-create time. Patch it afterwards with azapi_update_resource,
# which changes only the properties named in `body` and leaves AGIC's listeners,
# rules and backend pools alone.

resource "duploai_cluster_baseline" "azure_agic_buffers" {
  workspace_id = "<workspace-id>"
  name         = "aks-agic-buffers"
  cloud        = "Azure"
  network_id   = "<azure-network-baseline-id>"
  version      = "1.35"

  azure = {
    # Advanced AGIC path: you own the add-on entry, so enable_agic must be false
    # (it defaults to true, and the API rejects both being set together).
    enable_agic = false

    addon_profiles = {
      ingressApplicationGateway = {
        enabled = true

        # config is omitted on purpose. The platform stamps config.subnetId from
        # the linked network's ApplicationGateway subnet at create, and AKS then
        # creates a Standard_v2 gateway in it. Because config is computed,
        # omitting it keeps whatever the server set. Writing `config = {}`
        # instead sends an explicit empty map and clears those values.
      }
    }
  }
}

# azapi is a separate provider; declare it alongside duploai:
#
#   terraform {
#     required_providers {
#       azapi = {
#         source  = "Azure/azapi"
#         version = "~> 2.0"
#       }
#     }
#   }
#
# AKS publishes the gateway it created here, a few minutes after the cluster is
# Ready. Because the id is unknown until then, gate the patch on a static
# variable and apply a second time rather than deriving count from this value.
data "azapi_resource" "agic_cluster" {
  type        = "Microsoft.ContainerService/managedClusters@2024-09-01"
  resource_id = duploai_cluster_baseline.azure_agic_buffers.cluster_id

  response_export_values = [
    "properties.addonProfiles.ingressApplicationGateway.config.effectiveApplicationGatewayId",
  ]
}

resource "azapi_update_resource" "agic_appgw_buffers" {
  # api-version must be 2020-01-01 or later; globalConfiguration does not exist
  # in earlier versions (which is also why the Azure portal's JSON view can show
  # the property as absent while it is in fact set).
  type        = "Microsoft.Network/applicationGateways@2024-05-01"
  resource_id = data.azapi_resource.agic_cluster.output.properties.addonProfiles.ingressApplicationGateway.config.effectiveApplicationGatewayId

  body = {
    properties = {
      globalConfiguration = {
        enableRequestBuffering  = true
        enableResponseBuffering = false
      }
    }
  }
}
