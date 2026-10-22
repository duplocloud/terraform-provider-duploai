# The "shared" resource group owns S3 buckets, ECR repositories and a KMS key.
# Each grant below lets another resource group's IAM role use one of those areas.

# Same environment: the app resource group may use every S3 bucket in "shared".
resource "duploai_access_grant" "s3_to_app" {
  workspace_id              = "<workspace-id>"
  name                      = "shared-s3-to-app"
  environment_id            = "<shared-environment-id>"
  resource_group_id         = "<shared-resource-group-id>" # grantor: owns the buckets
  grantee_resource_group_id = "<app-resource-group-id>"    # grantee: gets access
  area                      = "S3"
}

# Cross environment: a resource group in another environment of the same
# workspace may pull every ECR image in "shared".
resource "duploai_access_grant" "ecr_to_prod" {
  workspace_id              = "<workspace-id>"
  name                      = "shared-ecr-to-prod"
  environment_id            = "<shared-environment-id>"
  resource_group_id         = "<shared-resource-group-id>"
  grantee_resource_group_id = "<prod-resource-group-id>" # in a different environment
  area                      = "Ecr"

  # Changing the grantee or area replaces the grant; create the new one first
  # so the grantee keeps access during the switch.
  lifecycle {
    create_before_destroy = true
  }
}

# KMS: the app resource group may encrypt and decrypt with "shared"'s key,
# e.g. to read objects in its KMS-encrypted buckets.
resource "duploai_access_grant" "kms_to_app" {
  workspace_id              = "<workspace-id>"
  name                      = "shared-kms-to-app"
  environment_id            = "<shared-environment-id>"
  resource_group_id         = "<shared-resource-group-id>"
  grantee_resource_group_id = "<app-resource-group-id>"
  area                      = "Kms"
}
