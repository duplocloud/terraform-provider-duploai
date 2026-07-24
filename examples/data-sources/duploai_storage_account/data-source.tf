# Look up an Azure storage account by ID.
data "duploai_storage_account" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_storage_account.example.status
}

output "azure_resource_id" {
  value = data.duploai_storage_account.example.azure_resource_id
}

output "primary_blob_endpoint" {
  value = data.duploai_storage_account.example.primary_blob_endpoint
}
