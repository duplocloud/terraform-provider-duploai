# A Kubernetes namespace in an EKS resource group
resource "duploai_k8s_namespace" "app" {
  workspace_id      = "<workspace-id>"
  name              = "my-app"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"

  description = "Namespace for the my-app workloads"

  # Tolerate up to 5 transient "Failed" polls during provisioning before giving
  # up. Optional — overrides the resource's built-in default.
  failure_retries = 5

  timeouts {
    create = "15m"
    delete = "10m"
  }
}
