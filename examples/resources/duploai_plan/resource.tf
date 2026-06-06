# A plan built on an existing network baseline.
#
# network_baseline_id is the raw backend id of the network baseline. Note that a
# duploai_network_baseline resource's `id` attribute is a COMPOSITE id
# ("<workspace-id>/<network-baseline-id>"), so pass the bare network baseline id
# here rather than referencing that resource's `id` directly.
resource "duploai_plan" "main" {
  workspace_id        = "<workspace-id>"
  name                = "prod-plan"
  scope_ids           = ["<scope-id>"]
  region              = "us-east-1"
  network_baseline_id = "<network-id>"

  timeouts {
    create = "30m"
    delete = "15m"
  }
}
