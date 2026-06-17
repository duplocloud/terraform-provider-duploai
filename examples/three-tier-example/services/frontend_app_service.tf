resource "duploai_app_service" "frontend" {
  workspace_id      = local.workspace_id
  name              = "frontend"
  scope_ids         = local.scope_ids
  resource_group_id = local.resource_group_id
  environment_id    = local.environment_id
  namespace_name    = local.namespace_name
  deployment_name   = "frontend"
  replicas          = 1

  containers = [
    {
      name  = "frontend"
      image = "public.ecr.aws/w8y4p9l8/demoapp-frontend:v2"
      ports = [{ container_port = 3000, name = "http", protocol = "TCP" }]
    },
  ]

  # The ClusterIP service the ingress routes to (assignment §5.1).
  service = {
    name  = "frontend"
    type  = "ClusterIP"
    ports = [{ port = 3000, target_port = 3000, protocol = "TCP", name = "http" }]
  }
}
