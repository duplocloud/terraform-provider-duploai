# Look up an EFS file system by workspace and ID.
data "duploai_aws_efs" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "file_system_id" {
  value = data.duploai_aws_efs.example.file_system_id
}

output "status" {
  value = data.duploai_aws_efs.example.status
}
