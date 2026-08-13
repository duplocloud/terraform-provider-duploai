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

# Helm release with install / upgrade behaviour tuned.
#
# `timeout` is the in-cluster Helm timeout Flux applies to each action; the
# `timeouts` block below is how long Terraform waits for the apply. Set both.
resource "duploai_helm_release" "ingress_nginx" {
  workspace_id      = "<workspace-id>"
  name              = "ingress-nginx"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"
  namespace_name    = "default"

  interval         = "10m"
  target_namespace = "ingress-nginx"
  timeout          = "10m"
  max_history      = 10

  chart_name            = "ingress-nginx"
  chart_version         = "4.x"
  chart_source_ref_kind = "HelmRepository"
  chart_source_ref_name = "ingress-nginx"

  install = {
    create_namespace = true
    crds             = "CreateReplace"
    timeout          = "15m"

    remediation = {
      retries = 3
    }
  }

  upgrade = {
    cleanup_on_fail = true
    crds            = "CreateReplace"

    remediation = {
      retries  = 2
      strategy = "rollback"
    }
  }

  rollback = {
    cleanup_on_fail = true
  }

  uninstall = {
    keep_history         = false
    deletion_propagation = "background"
  }

  test = {
    enable  = true
    timeout = "5m"

    filters = [
      {
        name = "ingress-nginx-test"
      }
    ]
  }

  # Correct cluster-side drift, but let the HPA own replica counts.
  drift_detection = {
    mode = "enabled"

    ignore = [
      {
        paths = ["/spec/replicas"]
        target = {
          kind = "Deployment"
        }
      }
    ]
  }

  timeouts {
    create = "30m"
    update = "30m"
  }
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
