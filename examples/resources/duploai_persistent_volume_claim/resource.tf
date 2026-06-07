# A ReadWriteOnce persistent volume claim in a namespace
resource "duploai_persistent_volume_claim" "data" {
  workspace_id      = "<workspace-id>"
  name              = "app-data"
  resource_group_id = "<eks-resource-group-id>"
  namespace_name    = "default"

  access_modes = ["ReadWriteOnce"]
  storage      = "10Gi"

  # Optional: pick a specific StorageClass; omit to use the cluster default.
  # storage_class_name = "gp3"
  volume_mode = "Filesystem"

  labels = {
    app = "myapp"
  }

  timeouts {
    create = "15m"
    delete = "10m"
  }
}
