# Look up cluster attributes by ID.
data "duploai_cluster_attributes" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_cluster_attributes.example.status
}

output "cluster_name" {
  value = data.duploai_cluster_attributes.example.cluster_name
}

output "vpc_id" {
  value = data.duploai_cluster_attributes.example.vpc_id
}
