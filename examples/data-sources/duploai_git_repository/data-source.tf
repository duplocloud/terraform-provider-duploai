# Look up a Git repository by ID.
data "duploai_git_repository" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_git_repository.example.status
}

output "url" {
  value = data.duploai_git_repository.example.url
}
