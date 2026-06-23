# Look up a scope by ID.
data "duploai_scope" "example" {
  id = "<object-id>"
}

output "name" {
  value = data.duploai_scope.example.name
}

output "provider_id" {
  value = data.duploai_scope.example.provider_id
}

output "is_active" {
  value = data.duploai_scope.example.is_active
}
