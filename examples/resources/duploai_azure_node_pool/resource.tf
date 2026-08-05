# A minimal pool: three fixed-size Linux nodes.
#
# The pool joins the AKS cluster linked to the resource group, so that resource group
# must already have a cluster attached and the cluster must have finished provisioning.
resource "duploai_azure_node_pool" "workers" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name       = "workers"
  vm_size    = "Standard_DS2_v2"
  node_count = 3
}

# An autoscaling pool spread across three availability zones.
#
# node_count is deliberately not set: while enable_auto_scaling is true Azure drives the
# count itself between min_count and max_count, and a value set here is not applied.
resource "duploai_azure_node_pool" "elastic" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name    = "elastic"
  vm_size = "Standard_D4s_v5"

  enable_auto_scaling = true
  min_count           = 2
  max_count           = 10

  availability_zones = ["1", "2", "3"]
}

# A dedicated pool for one workload: cheap Spot nodes, tainted so nothing else lands on
# them, and labelled so the workload can select them.
#
# Spot nodes cost less but Azure can evict them at any time, so only put
# interruption-tolerant workloads here.
#
# Use allocation_tag rather than writing the `allocationtags` label by hand — the
# platform manages that label and always overrides a value set in node_labels.
resource "duploai_azure_node_pool" "batch" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name               = "batch"
  vm_size            = "Standard_D8s_v5"
  scale_set_priority = "Spot"
  node_count         = 2

  allocation_tag = "batch-jobs"

  node_labels = {
    workload = "batch"
  }

  node_taints = [
    {
      key    = "workload"
      value  = "batch"
      effect = "NoSchedule"
    },
  ]
}

# A Windows pool. Azure caps a Windows pool's name at 6 characters.
#
# max_pods_per_node is immutable and bounded by the cluster's network plugin: with Azure
# CNI every pod takes a subnet IP address, so a high value needs a subnet large enough
# for max_pods_per_node × nodes.
resource "duploai_azure_node_pool" "winapp" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name    = "winapp"
  vm_size = "Standard_D4s_v5"
  os_type = "Windows"

  node_count        = 2
  max_pods_per_node = 60
}

# Rolling-upgrade behaviour. At most one of surge or unavailable may be active — the API
# rejects a setting where both are greater than zero.
resource "duploai_azure_node_pool" "surge" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name       = "surge"
  vm_size    = "Standard_DS2_v2"
  node_count = 4

  upgrade_settings = {
    max_surge_type  = "Percentage"
    max_surge_value = 33
  }
}

output "arm_resource_id" {
  value = duploai_azure_node_pool.workers.arm_resource_id
}

# The cluster the pool joined, resolved from the resource group's linked cluster. Note
# azure_cluster_resource_group_name is the CLUSTER's resource group, which is not
# necessarily the one named by resource_group_id.
output "cluster" {
  value = "${duploai_azure_node_pool.workers.azure_cluster_name} in ${duploai_azure_node_pool.workers.azure_cluster_resource_group_name}"
}
