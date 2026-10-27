# Basic Lambda function deployed from an S3 package.
#
# The platform currently accepts only S3 deployment packages: runtime, handler,
# s3_bucket and s3_key are all needed to create a function, and container-image
# functions (image_uri) are rejected.
resource "duploai_aws_lambda" "basic" {
  workspace_id      = "<workspace-id>"
  name              = "my-function"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  runtime  = "python3.12"
  handler  = "index.handler"
  role_arn = "<iam-role-arn>"

  s3_bucket = "<deployment-bucket>"
  s3_key    = "functions/my-function.zip"
}

# Lambda function with VPC access, environment variables, layers, and custom memory/timeout
resource "duploai_aws_lambda" "full" {
  workspace_id      = "<workspace-id>"
  name              = "api-processor"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  description       = "Processes API events and writes to RDS"

  runtime  = "python3.12"
  handler  = "app.handler"
  role_arn = "<iam-role-arn>"

  s3_bucket         = "<deployment-bucket>"
  s3_key            = "functions/api-processor.zip"
  s3_object_version = "<s3-version-id>"

  memory_size            = 512
  timeout                = 30
  ephemeral_storage_size = 1024

  architectures = ["arm64"]

  environment_variables = {
    LOG_LEVEL = "info"
    DB_HOST   = "<rds-endpoint>"
    REGION    = "us-east-1"
  }

  subnet_ids         = ["<subnet-id-1>", "<subnet-id-2>"]
  security_group_ids = ["<security-group-id>"]

  layers = ["<layer-arn>"]

  kms_key_arn = "<kms-key-arn>"

  tags = {
    env  = "production"
    team = "backend"
  }

  timeouts {
    create = "30m"
    update = "30m"
    delete = "15m"
  }
}
