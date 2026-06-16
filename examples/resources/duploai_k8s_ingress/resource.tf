resource "duploai_k8s_ingress" "example" {
  workspace_id       = var.workspace_id
  environment_id     = var.environment_id
  resource_group_id  = var.resource_group_id
  name               = "my-ingress"
  namespace_name     = "default"
  ingress_class_name = "nginx"

  annotations = {
    "nginx.ingress.kubernetes.io/rewrite-target" = "/"
  }

  rules = [
    {
      host = "api.example.com"
      http = {
        paths = [
          {
            path      = "/v1"
            path_type = "Prefix"
            backend = {
              service = {
                name = "api-service"
                port = {
                  number = 80
                }
              }
            }
          }
        ]
      }
    }
  ]

  tls = [
    {
      hosts       = ["api.example.com"]
      secret_name = "api-tls-secret"
    }
  ]
}
