# A simple nginx app service (Kubernetes Deployment)
resource "duploai_app_service" "nginx" {
  workspace_id      = "<workspace-id>"
  name              = "nginx"
  deployment_name   = "nginx"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  namespace_name    = "default"

  replicas = 2

  match_labels = { app = "nginx" }
  pod_labels   = { app = "nginx" }

  containers = [
    {
      name  = "nginx"
      image = "nginx:1.27"

      ports = [
        { container_port = 80 }
      ]

      env = [
        { name = "LOG_LEVEL", value = "info" }
      ]

      resources_requests = {
        cpu    = "250m"
        memory = "256Mi"
      }
      resources_limits = {
        cpu    = "500m"
        memory = "512Mi"
      }
    }
  ]

  timeouts {
    create = "20m"
    delete = "15m"
  }
}
