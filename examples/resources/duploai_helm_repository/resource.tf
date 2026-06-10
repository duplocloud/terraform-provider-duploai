# A public HTTPS Helm repository
resource "duploai_helm_repository" "bitnami" {
  workspace_id      = "<workspace-id>"
  name              = "bitnami"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"
  namespace_name    = "default"

  url      = "https://charts.bitnami.com/bitnami"
  interval = "10m"
}

# A private OCI Helm repository authenticated via a Kubernetes secret
resource "duploai_helm_repository" "private_oci" {
  workspace_id      = "<workspace-id>"
  name              = "internal-charts"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"
  namespace_name    = "default"

  url             = "oci://registry.example.com/charts"
  type            = "oci"
  oci_provider    = "generic"
  secret_ref_name = "registry-creds"

  labels = {
    team = "platform"
  }

  timeouts {
    create = "15m"
    delete = "10m"
  }
}
