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
