# Look up an ECR repository by workspace and ID.
data "duploai_ecr" "example" {
  workspace_id = "<workspace-id>"
  id           = "<ecr-id>"
}

output "repository_uri" {
  value = data.duploai_ecr.example.repository_uri
}

output "repository_arn" {
  value = data.duploai_ecr.example.repository_arn
}
