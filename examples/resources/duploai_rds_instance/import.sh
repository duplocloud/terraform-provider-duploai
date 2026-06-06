# Import an existing RDS instance resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - RDS_ID is the unique identifier of the RDS instance (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_rds_instance.mydb WORKSPACE_ID/RDS_ID
# Example:
# terraform import duploai_rds_instance.mydb 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
