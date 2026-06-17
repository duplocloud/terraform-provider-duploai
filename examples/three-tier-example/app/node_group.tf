# Kubernetes worker nodes via AutoScaling Group (assignment §2):
# t3a.medium, desired 1, min 1, max 2.
resource "duploai_node_group" "this" {
  workspace_id        = var.workspace_id
  name                = "${var.name_prefix}-asg"
  scope_ids           = var.scope_ids
  environment_id      = local.environment_id
  resource_group_id   = local.rg_id
  instance_visibility = "Internal"
  instance_types      = ["t3a.medium"]
  desired_size        = 1
  min_size            = 1
  max_size            = 2
  capacity_type       = "ON_DEMAND"
  ami_type            = "AL2023_x86_64_STANDARD"

}
