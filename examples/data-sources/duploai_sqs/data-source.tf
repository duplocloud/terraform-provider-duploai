# Look up an SQS queue by workspace and ID.
data "duploai_sqs" "example" {
  workspace_id = "<workspace-id>"
  id           = "<sqs-id>"
}

output "queue_url" {
  value = data.duploai_sqs.example.queue_url
}

output "queue_arn" {
  value = data.duploai_sqs.example.queue_arn
}
