# Look up an RDS cluster by ID.
data "duploai_rds_cluster" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_rds_cluster.example.status
}

output "cluster_endpoint" {
  value = data.duploai_rds_cluster.example.cluster_endpoint
}

output "port" {
  value = data.duploai_rds_cluster.example.port
}
