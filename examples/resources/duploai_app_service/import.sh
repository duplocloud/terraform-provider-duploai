# Import an existing app service resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - APP_SERVICE_ID is the unique identifier of the app service (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_app_service.nginx WORKSPACE_ID/APP_SERVICE_ID
# Example:
# terraform import duploai_app_service.nginx 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
