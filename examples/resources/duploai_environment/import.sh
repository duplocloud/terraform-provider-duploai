# Import an existing environment.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - ENVIRONMENT_ID is the unique identifier of the environment (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_environment.myenv WORKSPACE_ID/ENVIRONMENT_ID
# Example:
# terraform import duploai_environment.myenv 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
