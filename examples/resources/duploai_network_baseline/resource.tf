# Basic network baseline — Create mode, no NAT, no flow logs
resource "duploai_network_baseline" "basic" {
  workspace_id  = "<workspace-id>"
  name          = "prod-network"
  scope_ids     = ["<scope-id>"]
  region        = "us-east-1"
  cidr          = "10.0.0.0/16"
  az_count      = 2
  subnet_prefix = 24
}

# Network with NAT gateway and flow logs enabled
resource "duploai_network_baseline" "with_nat_and_logs" {
  workspace_id  = "<workspace-id>"
  name          = "prod-network-full"
  scope_ids     = ["<scope-id>"]
  region        = "us-east-1"
  cidr          = "10.1.0.0/16"
  az_count      = 2
  subnet_prefix = 24

  nat_mode                 = "SingleAz"
  enable_dns               = true
  enable_flow_logs         = true
  flow_logs_retention_days = 30

  timeouts {
    create = "45m"
    update = "30m"
    delete = "15m"
  }
}

# Custom subnet CIDRs — pick the subnet ranges yourself instead of letting the
# platform carve the VPC automatically. Both lists must be set, each with exactly
# az_count entries in AZ order: entry 0 lands in the first AZ, entry 1 in the
# second, and so on. Every range must sit inside cidr and not overlap any other.
# subnet_prefix is still required (the platform derives a template parameter from
# it) but is ignored for the carve-up once these lists are present.
resource "duploai_network_baseline" "custom_subnet_cidrs" {
  workspace_id  = "<workspace-id>"
  name          = "prod-network-custom-cidrs"
  scope_ids     = ["<scope-id>"]
  region        = "us-east-1"
  cidr          = "10.2.0.0/16"
  az_count      = 3
  subnet_prefix = 24

  custom_public_subnet_cidrs  = ["10.2.0.0/24", "10.2.1.0/24", "10.2.2.0/24"]
  custom_private_subnet_cidrs = ["10.2.16.0/20", "10.2.32.0/20", "10.2.48.0/20"]

  nat_mode   = "MultiAz"
  enable_dns = true

  timeouts {
    create = "45m"
  }
}

# Import an existing VPC (mode = "Import") — adopts a VPC the platform did not
# provision instead of creating one. Set vpc_id to the existing VPC; cidr and the
# other VPC details are read from it, so cidr is omitted.
resource "duploai_network_baseline" "imported" {
  workspace_id  = "<workspace-id>"
  name          = "imported-network"
  mode          = "Import"
  scope_ids     = ["<scope-id>"]
  region        = "us-east-1"
  vpc_id        = "vpc-0a1b2c3d4e5f67890"
  az_count      = 2
  subnet_prefix = 24
}

# Azure network baseline — provisions a virtual network, subnets, and a NAT
# gateway. az_count and subnet_prefix are AWS-only and omitted for Azure;
# per-subnet address prefixes are set explicitly. Delegations and NSG rules are
# seeded by the platform from each subnet's type.
resource "duploai_network_baseline" "azure" {
  workspace_id = "<workspace-id>"
  name         = "prod-network-azure"
  cloud        = "Azure"
  scope_ids    = ["<scope-id>"]
  region       = "eastus"
  cidr         = "10.0.0.0/16"

  azure = {
    preferred_subnet_mask = 22

    nat_gateways = [
      { name = "egress-nat" }
    ]

    subnets = [
      {
        name           = "general"
        subnet_type    = "GeneralPurpose"
        address_prefix = "10.0.1.0/24"
        # No default outbound — egress through the attached NAT gateway.
        default_outbound_access = false
        attach_nat              = true
        nat_gateway_name        = "egress-nat"
      },
      {
        name           = "app-gateway"
        subnet_type    = "ApplicationGateway"
        address_prefix = "10.0.2.0/24"
        # Azure-managed outbound; no NAT gateway attached.
        default_outbound_access = true
        attach_nat              = false
      }
    ]

    tags = {
      team = "platform"
    }
  }

  timeouts {
    create = "45m"
  }
}

# Full Azure network baseline — every azure option in use: an explicit resource
# group, custom DNS servers, additional address spaces, two NAT gateways, and
# subnets with explicit delegations and NSG security rules. Fields the platform
# manages (private_endpoint_network_policies, nat gateway sku, subnet/nsg IDs)
# are computed and never set here.
resource "duploai_network_baseline" "azure_full" {
  workspace_id = "<workspace-id>"
  name         = "prod-network-azure-full"
  cloud        = "Azure"
  scope_ids    = ["<scope-id>"]
  region       = "eastus"
  cidr         = "10.10.0.0/16"

  # Secondary virtual network address spaces beyond cidr.
  additional_cidrs = ["10.11.0.0/16"]

  azure = {
    resource_group_name   = "prod-network-rg"
    dns_servers           = ["10.10.0.4", "10.10.0.5"]
    preferred_subnet_mask = 24

    nat_gateways = [
      {
        name            = "egress-nat"
        public_ip_count = 2
      },
    ]

    subnets = [
      # General-purpose subnet: private, egressing through the NAT gateway,
      # with two custom inbound NSG rules on its own named NSG.
      {
        name                    = "general"
        subnet_type             = "GeneralPurpose"
        address_prefix          = "10.10.1.0/24"
        default_outbound_access = false
        attach_nat              = true
        nat_gateway_name        = "egress-nat"
        nsg_name                = "general-nsg"

        security_rules = [
          {
            name                       = "allow-https"
            description                = "Allow inbound HTTPS from anywhere"
            priority                   = 100
            direction                  = "Inbound"
            access                     = "Allow"
            protocol                   = "Tcp"
            source_address_prefix      = "*"
            source_port_range          = "*"
            destination_address_prefix = "*"
            destination_port_ranges    = ["443"]
          },
          {
            name                       = "allow-internal-app"
            description                = "Allow app ports from internal ranges"
            priority                   = 110
            direction                  = "Inbound"
            access                     = "Allow"
            protocol                   = "Tcp"
            source_address_prefixes    = ["10.10.0.0/16", "10.11.0.0/16"]
            source_port_range          = "*"
            destination_address_prefix = "*"
            destination_port_ranges    = ["8080", "8443"]
          },
        ]
      },
      # Delegated subnet for a PostgreSQL Flexible Server. The delegation hands
      # the subnet to the Azure service; keep it private (no default outbound).
      {
        name                    = "postgres"
        subnet_type             = "PostgresFlexibleServer"
        address_prefix          = "10.10.2.0/24"
        default_outbound_access = false
        attach_nat              = false

        delegations = [
          {
            name         = "postgres-delegation"
            service_name = "Microsoft.DBforPostgreSQL/flexibleServers"
          },
        ]
      },
    ]

    tags = {
      team        = "platform"
      environment = "production"
    }
  }

  timeouts {
    create = "60m"
    update = "45m"
    delete = "30m"
  }
}

# Import an existing Azure virtual network (mode = "Import"). Set
# azure.import_vnet_id to the VNet resource ID to adopt.
resource "duploai_network_baseline" "azure_imported" {
  workspace_id = "<workspace-id>"
  name         = "imported-network-azure"
  cloud        = "Azure"
  mode         = "Import"
  scope_ids    = ["<scope-id>"]
  region       = "eastus"

  azure = {
    import_vnet_id = "/subscriptions/<sub-id>/resourceGroups/<rg>/providers/Microsoft.Network/virtualNetworks/<vnet-name>"
  }
}
