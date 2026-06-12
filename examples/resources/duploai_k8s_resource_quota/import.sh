# Import an existing Kubernetes ResourceQuota resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - QUOTA_ID is the unique identifier of the resource quota (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_k8s_resource_quota.example WORKSPACE_ID/QUOTA_ID
# Example:
# terraform import duploai_k8s_resource_quota.example 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
