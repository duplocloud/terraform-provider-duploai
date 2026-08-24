# Look up an existing Azure Managed Redis instance.
data "duploai_azure_managed_redis" "cache" {
  workspace_id = "<workspace-id>"
  id           = "<redis-id>"
}

output "endpoint" {
  value = "${data.duploai_azure_managed_redis.cache.host_name}:${data.duploai_azure_managed_redis.cache.port}"
}

output "sku_name" {
  value = data.duploai_azure_managed_redis.cache.sku_name
}

# Empty until the `default` database child finishes provisioning, which happens after
# the cluster reports Complete.
output "database_id" {
  value = data.duploai_azure_managed_redis.cache.database_id
}

# Access keys are deliberately NOT exposed here. They are fetched live from Azure by a
# separate endpoint rather than stored on the object, and Azure rejects the call
# outright when access-key authentication is disabled (the default).
