# A ResourceQuota that limits CPU, memory, and pod count in a namespace
resource "duploai_k8s_resource_quota" "example" {
  workspace_id      = "<workspace-id>"
  name              = "team-quota"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"
  namespace_name    = "default"

  hard = {
    "requests.cpu"    = "4"
    "requests.memory" = "8Gi"
    "limits.cpu"      = "8"
    "limits.memory"   = "16Gi"
    "pods"            = "20"
  }

  # Optional: restrict quota to specific pod lifecycle scopes
  # scopes = ["NotTerminating"]

  labels = {
    team = "platform"
  }

  timeouts {
    create = "15m"
    delete = "10m"
  }
}
