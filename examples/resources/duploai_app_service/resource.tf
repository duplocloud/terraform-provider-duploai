# An nginx app service exposed via a Service + Ingress
resource "duploai_app_service" "nginx" {
  workspace_id      = "<workspace-id>"
  name              = "nginx"
  deployment_name   = "nginx"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"
  namespace_name    = "default"

  pod_labels = { app = "nginx" }

  containers = [
    {
      name  = "nginx"
      image = "nginx:1.27"
      ports = [{ container_port = 80 }]

      resources_requests = {
        cpu    = "250m"
        memory = "256Mi"
      }
    }
  ]

  # Expose the pods inside the cluster (the platform manages the selector).
  service = {
    name = "nginx"
    type = "ClusterIP"
    ports = [
      { port = 80, target_port = 80 }
    ]
  }

  # Route external traffic to the service.
  ingress = {
    name               = "nginx"
    ingress_class_name = "nginx"
    rules = [
      {
        host = "nginx.example.com"
        paths = [
          {
            path                = "/"
            path_type           = "Prefix"
            service_name        = "nginx"
            service_port_number = 80
          }
        ]
      }
    ]
  }

  # Autoscale the deployment on CPU utilization. When an HPA is set it owns the
  # replica count, so `replicas` is omitted above.
  # replication_type must be "Hpa" for the hpa block to be accepted.
  replication_type = "Hpa"
  hpa = {
    min_replicas = 2
    max_replicas = 10

    metrics = [
      {
        type = "Resource"
        resource = {
          name = "cpu"
          target = {
            type                = "Utilization"
            average_utilization = 70
          }
        }
      }
    ]
  }

  timeouts {
    create = "20m"
    delete = "15m"
  }
}
