# Basic resource group linked to an existing network baseline.
# The VPC is derived automatically from network_id by the API.
resource "duploai_resource_group" "basic" {
  workspace_id   = "<workspace-id>"
  name           = "<resource-group-name>"
  environment_id = "<environment-id>"
  scope_ids      = ["<scope-id>"]
  region         = "us-east-1"
  network_id     = "<network-baseline-id>"
}

# Resource group linked directly to a VPC (without a network baseline).
# Changing vpc_id forces replacement of the resource.
resource "duploai_resource_group" "from_vpc" {
  workspace_id   = "<workspace-id>"
  name           = "<resource-group-name>"
  environment_id = "<environment-id>"
  scope_ids      = ["<scope-id>"]
  region         = "us-east-1"
  vpc_id         = "<vpc-id>"
}

# Resource group with a custom provisioner and extended timeouts.
resource "duploai_resource_group" "custom" {
  workspace_id      = "<workspace-id>"
  name              = "<resource-group-name>"
  environment_id    = "<environment-id>"
  description       = "Production resource group with IaC-native provisioner"
  scope_ids         = ["<scope-id>"]
  region            = "us-east-1"
  network_id        = "<network-baseline-id>"
  provisioner_type  = "IacNativeTf"
  aws_resource_name = "prod-rg"

  timeouts {
    create = "45m"
    update = "30m"
    delete = "20m"
  }
}
