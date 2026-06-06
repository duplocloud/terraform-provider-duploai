# Import an existing cluster attributes resource.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 6a1578ae322a8a4142bbfa04)
#  - CLUSTER_ATTRIBUTES_ID is the ID of the cluster attributes resource (e.g. 6a23fee94703bc957a24eeb4)
terraform import duploai_cluster_attributes.basic WORKSPACE_ID/CLUSTER_ATTRIBUTES_ID
# Example:
# terraform import duploai_cluster_attributes.basic 6a1578ae322a8a4142bbfa04/6a23fee94703bc957a24eeb4
