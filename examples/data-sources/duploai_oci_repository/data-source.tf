# Look up an OCI repository by ID.
data "duploai_oci_repository" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_oci_repository.example.status
}
