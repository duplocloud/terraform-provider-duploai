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

# Disposable namespace — delete protection turned off up front so
# `terraform destroy` works without a second apply. Leaving delete_protection
# unset inherits the platform default (on), which makes destroy a two-step
# operation: set false, apply, then destroy.
#
# delete_protection is the only namespace field that changes in place; every
# other change replaces the namespace.
resource "duploai_k8s_namespace" "disposable" {
  workspace_id      = "<workspace-id>"
  name              = "ci-ephemeral-ns"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"

  delete_protection = false
}
