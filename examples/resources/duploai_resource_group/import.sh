# Import an existing resource group.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. c94d72b1e0538af6102d9e47)
#  - RESOURCE_GROUP_ID is the unique identifier of the resource group (e.g. f1a3047e8c92b5d6710e4f93)
terraform import duploai_resource_group.basic WORKSPACE_ID/RESOURCE_GROUP_ID
# Example:
# terraform import duploai_resource_group.basic c94d72b1e0538af6102d9e47/f1a3047e8c92b5d6710e4f93