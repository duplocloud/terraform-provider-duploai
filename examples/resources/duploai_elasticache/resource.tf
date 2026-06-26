# ── Redis, cluster mode DISABLED (single shard, 1 primary + replicas) ──────────
# cluster_mode=Disabled requires num_cache_clusters (1-6).
resource "duploai_elasticache" "redis_basic" {
  workspace_id      = "<workspace-id>"
  name              = "redis-basic"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"

  engine          = "Redis"
  engine_version  = "7.1"
  cache_node_type = "cache.t4g.micro"

  cluster_mode       = "Disabled"
  num_cache_clusters = 2 # 1 primary + 1 replica
}

# ── Redis, cluster mode ENABLED (sharded) ──────────────────────────────────────
# cluster_mode=Enabled requires num_node_groups (1-90) and replicas_per_node_group (0-5).
resource "duploai_elasticache" "redis_sharded" {
  workspace_id      = "<workspace-id>"
  name              = "redis-sharded"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"

  engine          = "Redis"
  engine_version  = "7.1"
  cache_node_type = "cache.r7g.large"

  cluster_mode            = "Enabled"
  num_node_groups         = 2 # 2 shards
  replicas_per_node_group = 1 # 1 replica each

  # Optional per-shard placement; length must equal num_node_groups when set.
  node_group_configuration = [
    {
      node_group_id              = "0001"
      primary_availability_zone  = "us-west-2a"
      replica_availability_zones = ["us-west-2b"]
      replica_count              = 1
    },
    {
      node_group_id              = "0002"
      primary_availability_zone  = "us-west-2b"
      replica_availability_zones = ["us-west-2c"]
      replica_count              = 1
    },
  ]
}

# ── Valkey (Redis-compatible), cluster mode ENABLED ────────────────────────────
resource "duploai_elasticache" "valkey" {
  workspace_id      = "<workspace-id>"
  name              = "valkey-cache"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"

  engine          = "Valkey"
  engine_version  = "8.0"
  cache_node_type = "cache.r7g.large"

  cluster_mode            = "Enabled"
  num_node_groups         = 3
  replicas_per_node_group = 2
}

# ── Memcached (multi-node, AZ-spread) ──────────────────────────────────────────
# engine=Memcached requires num_cache_nodes (1-40); uses az_mode, not cluster_mode.
resource "duploai_elasticache" "memcached" {
  workspace_id      = "<workspace-id>"
  name              = "session-cache"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"

  engine          = "Memcached"
  engine_version  = "1.6.22"
  cache_node_type = "cache.t4g.micro"

  num_cache_nodes              = 3
  az_mode                      = "cross-az"
  preferred_availability_zones = ["us-west-2a", "us-west-2b", "us-west-2c"]
}

# ── Redis with in-transit encryption + AUTH token ──────────────────────────────
# auth_token requires transit_encryption_enabled = true.
resource "duploai_elasticache" "redis_secure" {
  workspace_id      = "<workspace-id>"
  name              = "redis-secure"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"

  engine          = "Redis"
  engine_version  = "7.1"
  cache_node_type = "cache.t4g.small"

  cluster_mode       = "Disabled"
  num_cache_clusters = 2

  encryption_mode            = "ResourceGroupKmsKey" # at-rest via the RG's KMS key
  transit_encryption_enabled = true
  auth_token                 = "<redis-auth-token>" # sensitive

  # Tuning + observability
  preferred_maintenance_window = "sun:05:00-sun:06:00"
  snapshot_retention_limit     = 7
  snapshot_window              = "03:00-04:00"

  parameters = [
    {
      name  = "maxmemory-policy"
      value = "allkeys-lru"
    },
  ]
  log_delivery_configurations = [
    {
      log_type         = "slow-log"
      destination_type = "cloudwatch-logs"
      destination      = "/elasticache/redis-secure/slow"
      log_format       = "json"
    },
  ]

  timeouts {
    create = "60m"
    delete = "45m"
  }
}

# ── Redis restored from a snapshot ─────────────────────────────────────────────
# When snapshot_name is set, engine_version may be omitted (inferred from the snapshot).
# snapshot_name and snapshot_arns are mutually exclusive.
resource "duploai_elasticache" "redis_from_snapshot" {
  workspace_id      = "<workspace-id>"
  name              = "redis-restored"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"

  engine          = "Redis"
  cache_node_type = "cache.t4g.micro"

  cluster_mode       = "Disabled"
  num_cache_clusters = 1

  snapshot_name = "redis-basic-2024-06-01"
}
