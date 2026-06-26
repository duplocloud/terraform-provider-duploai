# ALB ingress exposing the frontend and backend via host-based routing
# (assignment §5.3). Uses the dedicated duploai_k8s_ingress resource; it routes
# to the ClusterIP services created by the two app_service resources.
resource "duploai_k8s_ingress" "demo" {
  workspace_id      = local.workspace_id
  name              = "demo-${local.name_prefix}"
  environment_id    = local.environment_id
  resource_group_id = local.resource_group_id
  namespace_name    = local.namespace_name

  ingress_class_name = "alb"
  annotations = {
    "alb.ingress.kubernetes.io/scheme"          = "internet-facing"
    "alb.ingress.kubernetes.io/target-type"     = "ip"
    "alb.ingress.kubernetes.io/certificate-arn" = "duplo-default/qa-apps.duplocloud.net"
  }

  rules = [
    {
      host = "frontend-${local.name_prefix}.qa-apps.duplocloud.net"
      http = {
        paths = [{
          path      = "/"
          path_type = "Prefix"
          backend = {
            service = {
              name = duploai_app_service.frontend.service_name
              port = { number = 3000 }
            }
          }
        }]
      }
    },
    {
      host = "backend-${local.name_prefix}.qa-apps.duplocloud.net"
      http = {
        paths = [{
          path      = "/"
          path_type = "Prefix"
          backend = {
            service = {
              name = duploai_app_service.backend.service_name
              port = { number = 3000 }
            }
          }
        }]
      }
    },
  ]

}
