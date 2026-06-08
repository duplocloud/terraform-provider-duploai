# An Opaque Kubernetes Secret with plaintext values (string_data)
resource "duploai_k8s_secret" "app" {
  workspace_id      = "<workspace-id>"
  name              = "app-secret"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  namespace_name    = "default"

  type = "Opaque"

  string_data = {
    DB_PASSWORD = "<password>"
    API_KEY     = "<api-key>"
  }

  labels = {
    app = "myapp"
  }

  timeouts {
    create = "15m"
    delete = "10m"
  }
}
