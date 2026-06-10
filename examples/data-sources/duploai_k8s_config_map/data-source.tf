# Look up a Kubernetes ConfigMap by ID.
data "duploai_k8s_config_map" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_k8s_config_map.example.status
}
