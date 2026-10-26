# Look up an access grant by ID.
data "duploai_access_grant" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "area" {
  value = data.duploai_access_grant.example.area
}

output "grantee_resource_group_id" {
  value = data.duploai_access_grant.example.grantee_resource_group_id
}

output "artifact_name" {
  value = data.duploai_access_grant.example.artifact_name
}
