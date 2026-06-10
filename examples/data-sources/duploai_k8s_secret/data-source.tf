# Look up a Kubernetes secret by ID.
data "duploai_k8s_secret" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_k8s_secret.example.status
}
