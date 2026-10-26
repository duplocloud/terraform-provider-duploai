# Import an existing SSM parameter.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - SSM_PARAMETER_ID is the unique identifier of the parameter (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_ssm_parameter.db_host WORKSPACE_ID/SSM_PARAMETER_ID
# Example:
# terraform import duploai_ssm_parameter.db_host 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
