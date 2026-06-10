# Look up a network baseline by ID.
data "duploai_network_baseline" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_network_baseline.example.status
}

output "vpc_id" {
  value = data.duploai_network_baseline.example.vpc_id
}

output "subnet_ids" {
  value = data.duploai_network_baseline.example.subnet_ids
}
