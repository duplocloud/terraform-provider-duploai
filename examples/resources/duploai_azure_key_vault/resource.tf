# Defaults: Standard tier, 90-day soft-delete retention, public endpoint open,
# purged on deprovision so the globally-unique name is immediately reusable.
resource "duploai_azure_key_vault" "example" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name = "my-app-vault"

  tags = {
    team = "platform"
  }
}

# Locked down: Premium tier, purge protection on, and the public endpoint
# restricted to an office range plus one subnet.
resource "duploai_azure_key_vault" "restricted" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name     = "my-secure-vault"
  sku_name = "Premium"

  soft_delete_retention_days = 90
  enable_purge_protection    = true

  # Purge protection blocks purging, so a destroyed vault keeps its name
  # reserved for the full retention period.
  purge_on_deprovision = false

  network_acls = {
    public_network_access = "Enabled"
    default_action        = "Deny"
    ip_rules              = ["203.0.113.0/24"]
    vnet_rules            = ["/subscriptions/<sub>/resourceGroups/<rg>/providers/Microsoft.Network/virtualNetworks/<vnet>/subnets/<subnet>"]
    bypass_azure_services = true
  }
}

# Private only — no public endpoint at all.
resource "duploai_azure_key_vault" "private" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  name = "my-private-vault"

  network_acls = {
    public_network_access = "Disabled"
  }
}
