# Look up a cluster baseline by ID.
data "duploai_cluster_baseline" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_cluster_baseline.example.status
}
