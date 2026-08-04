# A minimal instance: the smallest fixed-size SKU, TLS-only, no public endpoint.
resource "duploai_azure_managed_redis" "cache" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name     = "app-cache"
  sku_name = "Balanced_B1"

  tags = {
    app = "payments"
  }
}

# A larger instance with persistence. Cache size comes from the SKU name — every
# supported family is fixed-size, so there is no capacity/shard setting, and zone
# redundancy is automatic (Azure Managed Redis rejects explicit zones).
#
# The legacy Enterprise_* / EnterpriseFlash_* SKUs are NOT supported: those are Azure
# Cache for Redis Enterprise, which Azure has retired, and a create is rejected with
# "Creation of new Azure Cache for Redis Enterprise resources is no longer supported".
resource "duploai_azure_managed_redis" "bigger" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name     = "shared-cache"
  sku_name = "MemoryOptimized_M10"

  eviction_policy = "AllKeysLRU"

  persistence = {
    rdb_enabled   = true
    rdb_frequency = "SixHours"
  }
}

# Modules can only be enabled when the database is first created, so adding or
# removing one later forces replacement. RediSearch additionally requires
# EnterpriseCluster clustering. Everything else on the instance (tags, eviction
# policy, persistence) still updates in place.
resource "duploai_azure_managed_redis" "search" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name              = "search-cache"
  sku_name          = "Balanced_B10"
  clustering_policy = "EnterpriseCluster"

  modules = [
    { name = "RediSearch" },
    { name = "RedisBloom", args = "ERROR_RATE 0.01 INITIAL_SIZE 400" },
  ]
}

# Access keys are disabled by default, since Azure now steers new caches towards
# Microsoft Entra ID authentication. Enable them only if a client cannot use Entra —
# while disabled, the platform's key read/regenerate operations return an error
# because Azure rejects ListKeys.
#
# The setting can be changed in place. Note the platform's own console cannot: its
# save payload resends the current value rather than the newly selected one, so the
# toggle appears to revert. Terraform sends the value you configure.
#
# wait_for_database makes the apply block until the `default` database child exists.
# The cluster reports Complete first and the database is created afterwards, so
# without this `database_id` can still be empty when dependent resources run.
resource "duploai_azure_managed_redis" "with_keys" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name     = "legacy-client-cache"
  sku_name = "Balanced_B1"

  access_keys_authentication_enabled = true
  wait_for_database                  = true
}

# Active geo-replication. Azure only accepts these settings when the database is
# created, so a group is built by creating both members with it already configured:
# the first with a nickname and an empty link list, the second referencing the
# first's database_id. wait_for_database on the first is required — its database_id
# is empty until the database child finishes provisioning. Azure then links both
# ends itself, so the first member reports the second without Terraform sending it.
#
# WARNING: a geo-replicated instance cannot be UPDATED afterwards at all — not even
# its tags. Every update is rejected with "Cannot change spec.geoReplication after
# creation", because the platform compares the configured peer list against Azure's
# member list (which includes the database itself) and the two can never match.
# Replace the instance to change anything. Geo-replication also requires high
# availability, rejects the Balanced_B0/B1 and FlashOptimized_* SKUs, cannot be
# combined with persistence, and tolerates only the RediSearch and RedisJSON modules.
resource "duploai_azure_managed_redis" "geo_primary" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name              = "cache-east"
  sku_name          = "Balanced_B3"
  high_availability = true

  geo_replication = {
    group_nickname          = "prod-cache-group"
    linked_database_arm_ids = []
  }

  wait_for_database = true
}

resource "duploai_azure_managed_redis" "geo_secondary" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name              = "cache-west"
  sku_name          = "Balanced_B3"
  high_availability = true

  geo_replication = {
    group_nickname          = "prod-cache-group"
    linked_database_arm_ids = [duploai_azure_managed_redis.geo_primary.database_id]
  }

  wait_for_database = true
}

output "endpoint" {
  value = "${duploai_azure_managed_redis.cache.host_name}:${duploai_azure_managed_redis.cache.port}"
}

output "database_id" {
  value = duploai_azure_managed_redis.with_keys.database_id
}
