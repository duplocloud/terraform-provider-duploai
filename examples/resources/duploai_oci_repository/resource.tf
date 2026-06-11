# An OCI repository tracking a tag
resource "duploai_oci_repository" "podinfo" {
  workspace_id      = "<workspace-id>"
  name              = "podinfo"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"
  namespace_name    = "default"

  url      = "oci://ghcr.io/stefanprodan/manifests/podinfo"
  interval = "10m"
  ref_tag  = "latest"
}

# A private OCI repository pinned by semver, authenticated via a secret
resource "duploai_oci_repository" "internal" {
  workspace_id      = "<workspace-id>"
  name              = "internal-app"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"
  namespace_name    = "default"

  url             = "oci://registry.example.com/charts/app"
  interval        = "5m"
  ref_semver      = ">=1.0.0 <2.0.0"
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
