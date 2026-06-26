# Look up a native host by workspace and ID.
data "duploai_native_host" "example" {
  workspace_id = "<workspace-id>"
  id           = "<host-id>"
}

output "instance_id" {
  value = data.duploai_native_host.example.instance_id
}

output "private_ip_address" {
  value = data.duploai_native_host.example.private_ip_address
}

output "status" {
  value = data.duploai_native_host.example.status
}
