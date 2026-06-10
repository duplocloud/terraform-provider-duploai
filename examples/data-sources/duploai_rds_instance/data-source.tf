# Look up an RDS instance by ID.
data "duploai_rds_instance" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_rds_instance.example.status
}

output "stack_id" {
  value = data.duploai_rds_instance.example.stack_id
}
