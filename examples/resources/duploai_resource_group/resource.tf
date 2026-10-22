# Basic resource group linked to an existing network baseline.
# The VPC is derived automatically from network_id by the API.
resource "duploai_resource_group" "basic" {
  workspace_id   = "<workspace-id>"
  name           = "<resource-group-name>"
  environment_id = "<environment-id>"
  region         = ""
  network_id     = "<network-id>"
}

# Resource group linked directly to a VPC (without a network baseline).
# Changing vpc_id forces replacement of the resource.
resource "duploai_resource_group" "from_vpc" {
  workspace_id   = "<workspace-id>"
  name           = "<resource-group-name>"
  environment_id = "<environment-id>"
  region         = ""
  vpc_id         = "<vpc-id>"
}

# Resource group with a custom provisioner and extended timeouts.
resource "duploai_resource_group" "custom" {
  workspace_id      = "<workspace-id>"
  name              = "<resource-group-name>"
  environment_id    = "<environment-id>"
  description       = "Production resource group with IaC-native provisioner"
  region            = ""
  network_id        = "<network-id>"
  provisioner_type  = "IacNativeTf"
  aws_resource_name = "prod-rg"

  # User-defined tags, inherited by every resource provisioned under this group:
  # stamped on AWS resources as tags and on Kubernetes objects as labels. Each
  # entry must be legal in both systems, so the Kubernetes label grammar governs
  # the character set. The duplocloud.ai/ and aws: prefixes are reserved.
  #
  # Removing a key here removes the tag from every resource in the group, but
  # propagation is asynchronous — a removed tag can linger briefly after apply.
  # Omit the argument to leave stored tags untouched; set {} to remove them all.
  tags = {
    owner       = "platform-team"
    cost-center = "fin-1024"
  }

  # Free-form key/value metadata stored on the resource group record. Provide the
  # complete map — on update it replaces the previous one. Do not put
  # delete_protection here; that key belongs to the delete_protection attribute
  # and is filtered out of this map.
  metadata = {
    owner       = "platform-team"
    cost-center = "cc-4417"
  }

  timeouts {
    create = "45m"
    update = "30m"
    delete = "20m"
  }
}

# Disposable resource group — delete protection turned off up front so
# `terraform destroy` works without a second apply.
#
# The platform enables delete protection on every new resource group unless the
# create request says otherwise, and while it is on the API refuses both
# deprovision and delete. Leaving delete_protection unset therefore inherits the
# platform default (on), and tearing the group down then takes two steps: set it
# to false, apply, and only then destroy. Setting it here at create time skips
# that dance — appropriate for ephemeral/CI environments, not for production.
resource "duploai_resource_group" "disposable" {
  workspace_id   = "<workspace-id>"
  name           = "ci-ephemeral-rg"
  environment_id = "<environment-id>"
  region         = ""
  network_id     = "<network-id>"

  delete_protection = false
}

# Account that forbids IAM and KMS creation — the customer supplies an existing
# role and key. The baseline then creates no IAM role, managed policies, instance
# profile, KMS key or alias; it looks up the role's instance profile for EC2
# hosts, and every resource that defaults to the group key encrypts with this
# key. The key must be in the group's region. Both are customer-owned: the
# platform never edits or deletes them, so S3/ECR access grants to this group and
# KMS access grants from it are rejected. Removing either from the configuration
# keeps it; setting a different one replaces the group.
resource "duploai_resource_group" "customer_managed_iam" {
  workspace_id   = "<workspace-id>"
  name           = "<resource-group-name>"
  environment_id = "<environment-id>"
  region         = ""
  network_id     = "<network-id>"

  existing_iam_role_arn = "arn:aws:iam::123456789012:role/platform-shared"
  existing_kms_key_arn  = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
}
