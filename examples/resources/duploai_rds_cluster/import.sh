# Import an existing RDS cluster resource.
#  - WORKSPACE_ID is the MongoDB ObjectId of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - RDS_ID is the MongoDB ObjectId of the RDS cluster (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_rds_cluster.mydb WORKSPACE_ID/RDS_ID
# Example:
# terraform import duploai_rds_cluster.mydb 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
