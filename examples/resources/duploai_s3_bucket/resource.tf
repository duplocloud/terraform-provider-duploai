# A versioned, encrypted S3 bucket with public access blocked
resource "duploai_s3_bucket" "data" {
  workspace_id      = "<workspace-id>"
  name              = "my-app-data"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<resource-group-id>"
  environment_id    = "<environment-id>"
  region            = "us-west-2"

  enable_versioning = true
  encryption_type   = "SseKmsAwsManaged"
  object_ownership  = "BucketOwnerEnforced"

  block_public_access = {
    block_public_acls       = true
    ignore_public_acls      = true
    block_public_policy     = true
    restrict_public_buckets = true
  }

  cors_rules = [
    {
      allowed_methods = ["GET", "HEAD"]
      allowed_origins = ["https://app.example.com"]
      allowed_headers = ["*"]
      max_age_seconds = 3600
    }
  ]

  user_tags = {
    team = "platform"
  }

  timeouts {
    create = "30m"
    delete = "20m"
  }
}
