# Import an existing persistent volume claim resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - PVC_ID is the unique identifier of the persistent volume claim (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_persistent_volume_claim.data WORKSPACE_ID/PVC_ID
# Example:
# terraform import duploai_persistent_volume_claim.data 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
