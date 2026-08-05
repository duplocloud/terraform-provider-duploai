# Look up an existing Azure private endpoint.
data "duploai_azure_private_endpoint" "blob" {
  workspace_id = "<workspace-id>"
  id           = "<private-endpoint-id>"
}

# What the endpoint connects, and how.
output "connection" {
  value = {
    target       = data.duploai_azure_private_endpoint.blob.target_resource_arm_id
    sub_resource = data.duploai_azure_private_endpoint.blob.sub_resource_name
    subnet       = data.duploai_azure_private_endpoint.blob.subnet_id
  }
}

# The private address the target's hostname resolves to inside the network, and the DNS
# zone that makes that happen. dns_zone_group_provisioned being false means the endpoint
# is up but DNS still resolves publicly.
output "private_dns" {
  value = {
    ip          = data.duploai_azure_private_endpoint.blob.private_ip_address
    zone        = data.duploai_azure_private_endpoint.blob.dns_zone_name
    provisioned = data.duploai_azure_private_endpoint.blob.dns_zone_group_provisioned
  }
}

output "provisioning_state" {
  value = data.duploai_azure_private_endpoint.blob.provisioning_state
}
