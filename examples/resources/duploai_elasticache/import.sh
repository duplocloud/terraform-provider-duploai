# Import an existing ElastiCache cluster resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - ELASTICACHE_ID is the unique identifier of the cluster (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_elasticache.redis WORKSPACE_ID/ELASTICACHE_ID
# Example:
# terraform import duploai_elasticache.redis 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
