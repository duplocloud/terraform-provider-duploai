# Import an existing Lambda function resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - LAMBDA_ID is the unique identifier of the Lambda function (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_aws_lambda.my_function WORKSPACE_ID/LAMBDA_ID
# Example:
# terraform import duploai_aws_lambda.my_function 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
