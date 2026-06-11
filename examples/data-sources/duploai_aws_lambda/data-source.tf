# Look up an AWS Lambda function by ID.
data "duploai_aws_lambda" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_aws_lambda.example.status
}

output "function_arn" {
  value = data.duploai_aws_lambda.example.function_arn
}

output "code_sha256" {
  value = data.duploai_aws_lambda.example.code_sha256
}
