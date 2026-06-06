# A Kubernetes namespace in an EKS resource group
resource "duploai_namespace" "app" {
  workspace_id      = "<workspace-id>"
  name              = "my-app"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"

  description = "Namespace for the my-app workloads"

  timeouts {
    create = "15m"
    delete = "10m"
  }
}
