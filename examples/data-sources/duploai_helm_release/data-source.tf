# Look up a Helm release by ID.
data "duploai_helm_release" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_helm_release.example.status
}
