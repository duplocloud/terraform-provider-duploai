# Look up a provider by ID.
data "duploai_admin_provider" "example" {
  id = "<object-id>"
}

output "name" {
  value = data.duploai_admin_provider.example.name
}

output "type" {
  value = data.duploai_admin_provider.example.type
}

output "category" {
  value = data.duploai_admin_provider.example.category
}
