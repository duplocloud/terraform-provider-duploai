# Look up an S3 bucket by ID.
data "duploai_s3_bucket" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "bucket_arn" {
  value = data.duploai_s3_bucket.example.bucket_arn
}
