# Basic on-demand node group
resource "duploai_node_group" "basic" {
  workspace_id      = "<workspace-id>"
  name              = "app-nodes"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"

  instance_types            = ["t3.medium"]
  min_size                  = 1
  max_size                  = 3
  desired_size              = 2
  enable_cluster_autoscaler = true
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
  kms_key_id     = "<kms-key-arn>"
  capacity_type  = "SPOT"

  # ami_type is omitted on purpose: the platform picks one that suits the
  # cluster's Kubernetes version and these instance types' architecture. Set it
  # only to override that — and note AL2_* is rejected above Kubernetes 1.32.

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

  volumes = [
    {
      device_name    = "/dev/xvdb"
      volume_size_gb = 100
      volume_type    = "gp3"
    }
  ]

  tags = {
    team = "data-platform"
  }

  allocation_tag = "batch"

  timeouts {
    create = "30m"
    update = "30m"
    delete = "20m"
  }
}

# Node group on a custom AMI. ami_type = "CUSTOM" is the only value that accepts
# image_id, and it requires it. The AMI must derive from an EKS-optimised AL2023
# image so nodeadm is present — without it the nodes provision and then never
# join the cluster.
resource "duploai_node_group" "custom_ami" {
  workspace_id      = "<workspace-id>"
  name              = "custom-ami-nodes"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"

  instance_types = ["m5.large"]
  min_size       = 1
  max_size       = 3
  desired_size   = 1

  ami_type = "CUSTOM"
  image_id = "ami-0123456789abcdef0"
}
