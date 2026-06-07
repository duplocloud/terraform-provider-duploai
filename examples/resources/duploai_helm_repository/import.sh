# Import an existing Helm repository resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - HELM_REPOSITORY_ID is the unique identifier of the Helm repository (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_helm_repository.bitnami WORKSPACE_ID/HELM_REPOSITORY_ID
# Example:
# terraform import duploai_helm_repository.bitnami 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
