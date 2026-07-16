# An ECR repository with image scanning on push and immutable tags.
resource "duploai_ecr" "backend" {
  workspace_id      = "<workspace-id>"
  name              = "backend"
  repository_name   = "team/backend"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  encryption           = "AwsManagedKey"
  image_tag_mutability = "IMMUTABLE"
  scan_on_push         = true

  tags = [
    { key = "team", value = "platform" },
    { key = "app", value = "backend" },
  ]

  timeouts {
    create = "30m"
    delete = "15m"
  }
}

output "backend_repository_uri" {
  value = duploai_ecr.backend.repository_uri
}
