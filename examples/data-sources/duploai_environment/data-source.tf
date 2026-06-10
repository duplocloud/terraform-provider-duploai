# Look up an environment by ID.
data "duploai_environment" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_environment.example.status
}

output "is_active" {
  value = data.duploai_environment.example.is_active
}

output "version" {
  value = data.duploai_environment.example.version
}
