# Import an existing cluster baseline resource.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - CLUSTER_ID is the ID of the cluster baseline (e.g. 6a23cf3b4703bc957a23bbc0)
terraform import duploai_cluster_baseline.mycluster WORKSPACE_ID/CLUSTER_ID
# Example:
# terraform import duploai_cluster_baseline.mycluster 69b2aa30675718845bfe87a0/6a23cf3b4703bc957a23bbc0
