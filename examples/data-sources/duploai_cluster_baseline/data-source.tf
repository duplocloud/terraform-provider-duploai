# Look up a cluster baseline by ID.
data "duploai_cluster_baseline" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_cluster_baseline.example.status
}

output "cluster_endpoint" {
  value = data.duploai_cluster_baseline.example.cluster_endpoint
}

# Cloud-native cluster identifier — the EKS ARN on AWS or the AKS resource ID on Azure.
output "cluster_id" {
  value = data.duploai_cluster_baseline.example.cluster_id
}

# Azure (AKS) clusters expose additional provisioned identifiers under the azure_* outputs.
output "azure_fqdn" {
  value = data.duploai_cluster_baseline.example.azure_fqdn
}
