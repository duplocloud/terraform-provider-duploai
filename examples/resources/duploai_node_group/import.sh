# Import an existing node group resource.
#  - WORKSPACE_ID is the MongoDB ObjectId of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - NODE_GROUP_ID is the MongoDB ObjectId of the node group (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_node_group.mynodes WORKSPACE_ID/NODE_GROUP_ID
# Example:
# terraform import duploai_node_group.mynodes 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
