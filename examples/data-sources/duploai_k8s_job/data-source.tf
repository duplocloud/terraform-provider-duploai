# Look up a Kubernetes job by ID.
data "duploai_k8s_job" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_k8s_job.example.status
}
