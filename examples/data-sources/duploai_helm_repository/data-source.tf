# Look up a Helm repository by ID.
data "duploai_helm_repository" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_helm_repository.example.status
}
