# Look up an SNS topic by workspace and ID.
data "duploai_sns" "example" {
  workspace_id = "<workspace-id>"
  id           = "<sns-id>"
}

output "topic_arn" {
  value = data.duploai_sns.example.topic_arn
}

output "status" {
  value = data.duploai_sns.example.status
}
