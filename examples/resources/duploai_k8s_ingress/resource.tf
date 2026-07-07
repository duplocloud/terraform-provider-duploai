resource "duploai_k8s_ingress" "example" {
  workspace_id       = var.workspace_id
  environment_id     = var.environment_id
  resource_group_id  = var.resource_group_id
  name               = "my-ingress"
  namespace_name     = "default"
  ingress_class_name = "nginx"

  # Wait during apply until the cloud controller assigns a load balancer address
  # (exposed via the load_balancer attribute). Defaults to false, in which case
  # apply returns as soon as the ingress is created and the address populates on a
  # later refresh.
  wait_for_load_balancer = true

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
