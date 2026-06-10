# Import an existing storage class resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - STORAGE_CLASS_ID is the unique identifier of the storage class (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_k8s_storage_class.gp3 WORKSPACE_ID/STORAGE_CLASS_ID
# Example:
# terraform import duploai_k8s_storage_class.gp3 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
