data "duploai_k8s_ingress" "example" {
  workspace_id = var.workspace_id
  id           = var.ingress_id
}
