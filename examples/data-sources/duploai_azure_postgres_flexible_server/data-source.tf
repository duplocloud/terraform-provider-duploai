# Look up an Azure PostgreSQL Flexible Server by ID.
data "duploai_azure_postgres_flexible_server" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_azure_postgres_flexible_server.example.status
}

output "fully_qualified_domain_name" {
  value = data.duploai_azure_postgres_flexible_server.example.fully_qualified_domain_name
}

output "provisioning_state" {
  value = data.duploai_azure_postgres_flexible_server.example.provisioning_state
}

output "postgres_server_id" {
  value = data.duploai_azure_postgres_flexible_server.example.postgres_server_id
}
