# Import an existing Azure Managed Redis instance.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - REDIS_ID is the ID of the Redis instance (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_azure_managed_redis.cache WORKSPACE_ID/REDIS_ID
# Example:
# terraform import duploai_azure_managed_redis.cache 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
#
# wait_for_database is a Terraform-only control attribute — it drives how long an
# apply waits and is not stored by the API, so it lands false after an import. Set it
# in config if you want it; that produces no API call.
