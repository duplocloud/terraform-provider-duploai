# Look up an SSM parameter by ID.
data "duploai_ssm_parameter" "example" {
  workspace_id = "<workspace-id>"
  id           = "<ssm-parameter-id>"
}

output "arn" {
  value = data.duploai_ssm_parameter.example.arn
}

output "version" {
  value = data.duploai_ssm_parameter.example.version
}
