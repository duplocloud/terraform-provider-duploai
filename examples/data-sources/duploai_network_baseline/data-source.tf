# Look up a network baseline by ID.
data "duploai_network_baseline" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_network_baseline.example.status
}

output "vpc_id" {
  value = data.duploai_network_baseline.example.vpc_id
}

output "subnet_ids" {
  value = data.duploai_network_baseline.example.subnet_ids
}

# Azure networks expose their provisioned identifiers under the azure_* outputs.
output "azure_vnet_id" {
  value = data.duploai_network_baseline.example.azure_vnet_id
}

output "azure_resource_group" {
  value = data.duploai_network_baseline.example.azure_resource_group
}

output "azure_subscription_id" {
  value = data.duploai_network_baseline.example.azure_subscription_id
}

output "azure_subnet_ids" {
  value = data.duploai_network_baseline.example.azure_subnet_ids
}

output "azure_nat_gateway_ids" {
  value = data.duploai_network_baseline.example.azure_nat_gateway_ids
}
