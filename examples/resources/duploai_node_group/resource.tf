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
# image_id, and it requires it. EKS supplies no bootstrap configuration for a
# custom AMI, so the platform generates one — detecting nodeadm or the classic
# /etc/eks/bootstrap.sh from the AMI name and the cluster version — and the
# launch template carries it.
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

# ── Running your own script on the nodes: user_data ──────────────────────────
#
# Which combination you want:
#
#   ami_type   user_data_mode   what happens
#   ────────   ──────────────   ────────────────────────────────────────────────
#   managed    (ignored)        EKS bootstraps the node and merges your script
#                               alongside it. Your script must NOT start or
#                               reconfigure kubelet — that is EKS's job.
#   CUSTOM     "Append"         The platform generates the bootstrap AND runs
#                               your script. Pick this to ADD setup steps.
#   CUSTOM     "Override"       The platform generates NOTHING. Your script
#                               alone must join the node to the cluster.
#
# "Override" is the default, so a script that only installs an agent, with the
# mode left unset on a CUSTOM AMI, leaves the node running and never joined to
# the cluster. Set "Append" for that case.
#
# user_data is plain text — the platform base64-encodes it, so do not encode it
# yourself (the opposite of duploai_native_host.base64_user_data). It is capped
# at 1024 bytes and marked sensitive: bootstrap scripts routinely carry registry
# credentials or join tokens, so keep real secrets in a variable, not inline.

# CUSTOM AMI, adding setup steps on top of the generated bootstrap.
resource "duploai_node_group" "custom_ami_bootstrap" {
  workspace_id      = "<workspace-id>"
  name              = "custom-ami-bootstrap"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"

  instance_types = ["m5.large"]
  min_size       = 1
  max_size       = 3
  desired_size   = 1

  ami_type = "CUSTOM"
  image_id = "ami-0123456789abcdef0"

  user_data_mode = "Append"
  user_data      = <<-EOT
    #!/bin/bash
    echo "fs.inotify.max_user_watches=524288" >> /etc/sysctl.conf
    sysctl -p
  EOT
}

# Managed AMI type, with an extra step. No ami_type or user_data_mode needed:
# EKS bootstraps the node and merges this script alongside its own.
resource "duploai_node_group" "managed_ami_user_data" {
  workspace_id      = "<workspace-id>"
  name              = "managed-ami-user-data"
  scope_ids         = ["<scope-id>"]
  resource_group_id = "<eks-resource-group-id>"
  environment_id    = "<environment-id>"

  instance_types = ["m5.large"]
  min_size       = 1
  max_size       = 3
  desired_size   = 1

  user_data = <<-EOT
    #!/bin/bash
    mkdir -p /opt/telemetry && touch /opt/telemetry/enabled
  EOT
}
