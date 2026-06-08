# Import an existing config map resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - CONFIG_MAP_ID is the unique identifier of the config map (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_k8s_config_map.app WORKSPACE_ID/CONFIG_MAP_ID
# Example:
# terraform import duploai_k8s_config_map.app 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
