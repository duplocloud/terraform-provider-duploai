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

  service = {
    name  = "frontend"
    type  = "ClusterIP"
    ports = [{ port = 3000, target_port = 3000, protocol = "TCP", name = "http" }]
  }

  # Single ALB ingress with host-based rules for both services (assignment §5).
  ingress = {
    name               = "demo-${local.name_prefix}"
    ingress_class_name = "alb"
    annotations = {
      "alb.ingress.kubernetes.io/scheme"      = "internet-facing"
      "alb.ingress.kubernetes.io/target-type" = "ip"
    }
    rules = [
      {
        host  = "frontend-${local.name_prefix}.qa-apps.duplocloud.net"
        paths = [{ path = "/", path_type = "Prefix", service_name = "frontend", service_port_number = 3000 }]
      },
      {
        host  = "backend-${local.name_prefix}.qa-apps.duplocloud.net"
        paths = [{ path = "/", path_type = "Prefix", service_name = "backend", service_port_number = 3000 }]
      },
    ]
  }
}
