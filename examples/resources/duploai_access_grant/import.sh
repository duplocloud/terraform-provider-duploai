# Import an existing access grant.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - ACCESS_GRANT_ID is the unique identifier of the access grant (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_access_grant.s3_to_app WORKSPACE_ID/ACCESS_GRANT_ID
# Example:
# terraform import duploai_access_grant.s3_to_app 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
