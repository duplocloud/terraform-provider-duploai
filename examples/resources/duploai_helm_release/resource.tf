# Helm release from a HelmRepository chart source
resource "duploai_helm_release" "podinfo" {
  workspace_id      = "<workspace-id>"
  name              = "podinfo"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
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
  namespace_name    = "default"

  interval = "5m"

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
