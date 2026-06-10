# Look up an AWS Secrets Manager secret by ID.
data "duploai_aws_secret" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_aws_secret.example.status
}

output "arn" {
  value = data.duploai_aws_secret.example.arn
}
