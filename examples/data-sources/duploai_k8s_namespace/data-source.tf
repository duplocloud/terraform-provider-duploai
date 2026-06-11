# Look up a Kubernetes namespace by ID.
data "duploai_k8s_namespace" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_k8s_namespace.example.status
}
