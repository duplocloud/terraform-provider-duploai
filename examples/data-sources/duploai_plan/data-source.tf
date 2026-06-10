# Look up a plan by ID.
data "duploai_plan" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_plan.example.status
}

output "primary_hosted_zone_domain" {
  value = data.duploai_plan.example.primary_hosted_zone_domain
}
