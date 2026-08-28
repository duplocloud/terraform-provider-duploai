# Basic on-demand node group
resource "duploai_node_group" "basic" {
  workspace_id      = "<workspace-id>"
  name              = "app-nodes"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"

  instance_types = ["t3.medium"]
  min_size       = 1
  max_size       = 3
  desired_size   = 2
}

# Spot node group with disk sizing, labels, and a taint
resource "duploai_node_group" "spot" {
  workspace_id      = "<workspace-id>"
  name              = "batch-spot"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"

  instance_types = ["m5.large", "m5a.large"]
  min_size       = 0
  max_size       = 10
  desired_size   = 2
  disk_size_gb   = 100
  capacity_type  = "<capacity-type>"
  ami_type       = "<ami-type>"
  image_id       = "<ami-id>"

  additional_labels = {
    workload = "batch"
  }

  taints = [
    {
      key    = "dedicated"
      value  = "batch"
      effect = "<taint-effect>"
    }
  ]

  allocation_tag = "batch"

  timeouts {
    create = "30m"
    update = "30m"
    delete = "20m"
  }
}
