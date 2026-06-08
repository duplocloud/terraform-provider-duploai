# Import an existing OCI repository resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - OCI_REPOSITORY_ID is the unique identifier of the OCI repository (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_oci_repository.podinfo WORKSPACE_ID/OCI_REPOSITORY_ID
# Example:
# terraform import duploai_oci_repository.podinfo 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
