# Import an existing network baseline resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - NETWORK_ID is the unique identifier of the network baseline (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_network_baseline.mynetwork WORKSPACE_ID/NETWORK_ID
# Example:
# terraform import duploai_network_baseline.mynetwork 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
