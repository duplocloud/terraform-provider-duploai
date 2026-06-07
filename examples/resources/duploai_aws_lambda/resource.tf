# Basic Lambda function deployed from an S3 package
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

# Container image Lambda (no runtime or handler required)
resource "duploai_aws_lambda" "container" {
  workspace_id      = "<workspace-id>"
  name              = "ml-inference"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  description       = "ML inference function using a custom container image"

  role_arn  = "<iam-role-arn>"
  image_uri = "<account-id>.dkr.ecr.<region>.amazonaws.com/<repo>:<tag>"

  memory_size = 3008
  timeout     = 60

  environment_variables = {
    MODEL_PATH = "/opt/ml/model"
  }
}
