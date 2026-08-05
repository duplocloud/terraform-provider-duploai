# A private endpoint for a storage account's blob service.
#
# The target must already exist, so reference it rather than hardcoding an ARM ID —
# that also makes Terraform create the storage account first.
resource "duploai_azure_private_endpoint" "blob" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name                   = "app-blob-pe"
  target_resource_arm_id = duploai_storage_account.app.azure_resource_id
  sub_resource_name      = "blob"
  subnet_id              = duploai_network_baseline.main.azure_subnet_ids[0]

  tags = {
    app = "payments"
  }
}

# One endpoint serves one sub-resource, so a storage account that needs both blob and
# file access privately needs two endpoints. They share a subnet and each takes its own
# private IP address from it.
resource "duploai_azure_private_endpoint" "file" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name                   = "app-file-pe"
  target_resource_arm_id = duploai_storage_account.app.azure_resource_id
  sub_resource_name      = "file"
  subnet_id              = duploai_network_baseline.main.azure_subnet_ids[0]
}

# A Key Vault endpoint. The sub-resource name is `vault`, which the platform maps to the
# privatelink.vaultcore.azure.net DNS zone.
resource "duploai_azure_private_endpoint" "vault" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name                   = "app-vault-pe"
  target_resource_arm_id = duploai_azure_key_vault.app.key_vault_id
  sub_resource_name      = "vault"
  subnet_id              = duploai_network_baseline.main.azure_subnet_ids[0]
}

# An Azure Managed Redis endpoint, targeting the cluster's ARM ID.
#
# Pair it with public_network_access_enabled = false on the cache, so the only route in
# is this endpoint.
resource "duploai_azure_private_endpoint" "redis" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name                   = "app-redis-pe"
  target_resource_arm_id = duploai_azure_managed_redis.cache.cluster_id
  sub_resource_name      = "redisenterprise"
  subnet_id              = duploai_network_baseline.main.azure_subnet_ids[0]
}

# The private IP the target's hostname now resolves to from inside the network. Empty
# until Azure finishes provisioning, which is why it is worth outputting rather than
# assuming.
output "blob_private_ip" {
  value = duploai_azure_private_endpoint.blob.private_ip_address
}

# dns_zone_group_provisioned is the one to assert on: the endpoint only reaches Complete
# once its DNS zone group is attached, so this being true means the target's public
# hostname resolves privately. While false, the endpoint exists but DNS still points at
# the public address.
output "blob_dns" {
  value = {
    zone        = duploai_azure_private_endpoint.blob.dns_zone_name
    provisioned = duploai_azure_private_endpoint.blob.dns_zone_group_provisioned
  }
}
