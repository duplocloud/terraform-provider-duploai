# Import an existing Azure node pool.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - NODE_POOL_ID is the ID of the node pool (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_azure_node_pool.workers WORKSPACE_ID/NODE_POOL_ID
# Example:
# terraform import duploai_azure_node_pool.workers 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
