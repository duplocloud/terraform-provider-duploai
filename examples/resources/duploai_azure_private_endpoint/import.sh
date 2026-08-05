# Import an existing Azure private endpoint.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - PRIVATE_ENDPOINT_ID is the ID of the private endpoint (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_azure_private_endpoint.blob WORKSPACE_ID/PRIVATE_ENDPOINT_ID
# Example:
# terraform import duploai_azure_private_endpoint.blob 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
