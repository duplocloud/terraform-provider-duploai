# Import an existing S3 bucket resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - S3_BUCKET_ID is the unique identifier of the S3 bucket (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_s3_bucket.data WORKSPACE_ID/S3_BUCKET_ID
# Example:
# terraform import duploai_s3_bucket.data 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
