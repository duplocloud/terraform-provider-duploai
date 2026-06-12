# Look up a Kubernetes ResourceQuota by ID.
data "duploai_k8s_resource_quota" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_k8s_resource_quota.example.status
}
