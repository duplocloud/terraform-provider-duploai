# Look up a quota mapping by ID.
data "duploai_admin_quota_mapping" "example" {
  id = "<object-id>"
}

output "scope" {
  value = data.duploai_admin_quota_mapping.example.scope
}

output "quota_reference_id" {
  value = data.duploai_admin_quota_mapping.example.quota_reference_id
}
