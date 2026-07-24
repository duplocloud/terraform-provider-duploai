# Minimal Azure Storage Account — provisioned in an environment + resource group.
# sku_name, kind, access tier, TLS, and data protection all default.
resource "duploai_storage_account" "basic" {
  workspace_id      = "<workspace-id>"
  name              = "prodsa001" # 3-24 chars, lowercase alphanumeric, globally unique
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
}

# Full example — geo-redundant, hardened, Data Lake Gen2 (HNS) with SFTP, custom
# data protection, and tags. sku_name / kind / is_hns_enabled / is_nfs_v3_enabled
# are immutable (changing them replaces the account).
resource "duploai_storage_account" "full" {
  workspace_id      = "<workspace-id>"
  name              = "proddatalake01"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  sku_name                  = "Standard_GRS"
  access_tier               = "Hot"
  minimum_tls_version       = "TLS1_2"
  enable_https_traffic_only = true
  allow_blob_public_access  = false

  # Data Lake Gen2 with SFTP (both require hierarchical namespace).
  is_hns_enabled  = true
  is_sftp_enabled = true

  data_protection = {
    enable_blob_soft_delete         = true
    blob_soft_delete_retention_days = 14
    enable_container_soft_delete    = true
    enable_versioning               = true
    enable_point_in_time_restore    = true
  }

  tags = {
    team        = "platform"
    environment = "production"
  }

  timeouts {
    create = "30m"
    update = "20m"
    delete = "20m"
  }
}
