# Look up an existing Azure node pool.
data "duploai_azure_node_pool" "workers" {
  workspace_id = "<workspace-id>"
  id           = "<node-pool-id>"
}

output "vm_size" {
  value = data.duploai_azure_node_pool.workers.vm_size
}

# Sizing as the platform has it recorded. While enable_auto_scaling is true the live node
# count is whatever Azure has scaled to, which this does not report — the API withholds
# the raw cloud snapshot from its responses, so only min_count and max_count are
# meaningful here.
output "sizing" {
  value = {
    node_count          = data.duploai_azure_node_pool.workers.node_count
    min_count           = data.duploai_azure_node_pool.workers.min_count
    max_count           = data.duploai_azure_node_pool.workers.max_count
    enable_auto_scaling = data.duploai_azure_node_pool.workers.enable_auto_scaling
  }
}

# The AKS cluster hosting the pool.
output "cluster" {
  value = "${data.duploai_azure_node_pool.workers.azure_cluster_name} in ${data.duploai_azure_node_pool.workers.azure_cluster_resource_group_name}"
}
