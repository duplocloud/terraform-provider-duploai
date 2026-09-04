# A regional, encrypted EFS file system with elastic throughput.
resource "duploai_aws_efs" "shared" {
  workspace_id      = "<workspace-id>"
  name              = "shared-data"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"

  # Switch to ResourceGroupKmsKey and set kms_key_id to use a registered
  # customer-managed key; leaving kms_key_id unset uses the resource group's
  # own default key.
  encryption       = "AwsManagedKey"
  performance_mode = "generalPurpose"
  throughput_mode  = "elastic"

  tags = [
    { key = "team", value = "platform" }
  ]
}

# A provisioned-throughput file system (throughput value required in this mode).
resource "duploai_aws_efs" "db_backups" {
  workspace_id      = "<workspace-id>"
  name              = "db-backups"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"

  throughput_mode                 = "provisioned"
  provisioned_throughput_in_mibps = 50
}
