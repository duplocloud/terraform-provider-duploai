# A Kubernetes ConfigMap with string data
resource "duploai_config_map" "app" {
  workspace_id      = "<workspace-id>"
  name              = "app-config"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  namespace_name    = "default"

  data = {
    LOG_LEVEL    = "info"
    FEATURE_FLAG = "true"
  }

  labels = {
    app = "myapp"
  }

  timeouts {
    create = "15m"
    delete = "10m"
  }
}
