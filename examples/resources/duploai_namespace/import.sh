# Import an existing namespace resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - NAMESPACE_ID is the unique identifier of the namespace (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_namespace.app WORKSPACE_ID/NAMESPACE_ID
# Example:
# terraform import duploai_namespace.app 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
