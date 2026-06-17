resource "duploai_cluster_baseline" "this" {
  workspace_id          = var.workspace_id
  name                  = "${var.name_prefix}-cluster"
  description           = "Demo app EKS cluster"
  network_id            = duploai_network_baseline.this.network_id
  eks_version           = var.eks_version
  api_server_visibility = "PublicAndPrivate"

  timeouts {
    create = "45m"
    delete = "30m"
  }
}
