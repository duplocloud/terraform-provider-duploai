# Import an existing EFS file system.
#  - WORKSPACE_ID is the ID of the workspace that owns the file system.
#  - EFS_ID is the unique identifier of the EFS resource (e.g. 6a2258e94703bc957a1b824e).
terraform import duploai_aws_efs.shared WORKSPACE_ID/EFS_ID
# Example:
# terraform import duploai_aws_efs.shared 6a25105705686d697e0da225/6a2258e94703bc957a1b824e
