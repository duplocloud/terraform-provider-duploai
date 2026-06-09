# Import an existing AWS secret resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - AWS_SECRET_ID is the unique identifier of the secret (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_aws_secret.db_password WORKSPACE_ID/AWS_SECRET_ID
# Example:
# terraform import duploai_aws_secret.db_password 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
