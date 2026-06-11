# Look up an app service by ID.
data "duploai_app_service" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_app_service.example.status
}

output "service_name" {
  value = data.duploai_app_service.example.service_name
}

output "ingress_name" {
  value = data.duploai_app_service.example.ingress_name
}
