# Import an existing Helm release resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - HELM_RELEASE_ID is the unique identifier of the Helm release (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_helm_release.podinfo WORKSPACE_ID/HELM_RELEASE_ID
# Example:
# terraform import duploai_helm_release.podinfo 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
