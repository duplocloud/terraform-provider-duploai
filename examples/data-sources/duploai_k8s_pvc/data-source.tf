# Look up a Kubernetes PersistentVolumeClaim by ID.
data "duploai_k8s_pvc" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_k8s_pvc.example.status
}
