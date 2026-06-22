# Look up a quota definition by ID.
data "duploai_admin_quota_definition" "example" {
  id = "<object-id>"
}

output "name" {
  value = data.duploai_admin_quota_definition.example.name
}

output "limit_usd" {
  value = data.duploai_admin_quota_definition.example.limit_usd
}
