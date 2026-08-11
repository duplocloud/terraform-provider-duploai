# Helm release from a HelmRepository chart source
resource "duploai_helm_release" "podinfo" {
  workspace_id      = "<workspace-id>"
  name              = "podinfo"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"
  namespace_name    = "default"

  interval         = "10m"
  target_namespace = "default"

  chart_name            = "podinfo"
  chart_version         = "6.x"
  chart_source_ref_kind = "HelmRepository"
  chart_source_ref_name = "podinfo"

  values = <<-YAML
    replicaCount: 2
    ui:
      message: "hello from terraform"
  YAML
}

# Helm release from an OCIRepository chart, with values from a ConfigMap
resource "duploai_helm_release" "app" {
  workspace_id      = "<workspace-id>"
  name              = "app"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"
  namespace_name    = "default"

  interval = "5m"

  # How long Helm itself waits for install/upgrade to report ready, and how
  # many times Flux retries a failed attempt before marking the release
  # Stalled (both default to 5m / 0 retries when unset). Raise these for
  # charts whose pods are slow to pass readiness on a scaling node pool.
  timeout         = "15m"
  install_retries = 2
  upgrade_retries = 2

  chart_ref_kind = "OCIRepository"
  chart_ref_name = "app"

  values_from = [
    {
      kind       = "ConfigMap"
      name       = "app-values"
      values_key = "values.yaml"
    }
  ]

  timeouts {
    create = "30m"
    delete = "15m"
  }
}
