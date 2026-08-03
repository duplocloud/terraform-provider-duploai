# A plan built on an existing network baseline.
#
# network_baseline_id is the raw backend id of the network baseline. Note that a
# duploai_network_baseline resource's `id` attribute is a COMPOSITE id
# ("<workspace-id>/<network-baseline-id>"), so pass the bare network baseline id
# here rather than referencing that resource's `id` directly.
resource "duploai_plan" "main" {
  workspace_id        = "<workspace-id>"
  name                = "prod-plan"
  scope_ids           = ["<scope-id>"]
  region              = "us-east-1"
  network_baseline_id = "<network-id>"

  timeouts {
    create = "30m"
    delete = "15m"
  }
}

# A plan that brings an existing DNS hosted zone and ACM certificates instead of
# letting the platform provision them. Leave these unset (as in "main" above) to
# have the platform create the hosted zone and certificates automatically.
resource "duploai_plan" "byo_dns" {
  workspace_id        = "<workspace-id>"
  name                = "prod-plan-byo"
  scope_ids           = ["<scope-id>"]
  region              = "us-east-1"
  network_baseline_id = "<network-id>"

  # Existing Route 53 hosted zone.
  primary_hosted_zone_id     = "Z3P5QSUBK4POTI"
  primary_hosted_zone_domain = "example.com"

  # Existing ACM certificates (ARN + the domain each covers).
  certificates = [
    {
      certificate_arn = "arn:aws:acm:us-east-1:123456789012:certificate/11111111-1111-1111-1111-111111111111"
      domain          = "example.com"
    },
    {
      certificate_arn = "arn:aws:acm:us-east-1:123456789012:certificate/22222222-2222-2222-2222-222222222222"
      domain          = "api.example.com"
    },
  ]

  timeouts {
    create = "30m"
    delete = "15m"
  }
}

# An Azure plan registering existing Key Vault certificates for the AGIC
# Application Gateway to serve.
#
# These are REFERENCES, not certificates: the platform never creates one. Leaving
# azure_certificates unset means no certificates are registered, not that the
# platform will provide them.
resource "duploai_plan" "azure_byo_cert" {
  workspace_id        = "<workspace-id>"
  name                = "prod-plan-azure"
  scope_ids           = ["<scope-id>"]
  region              = "westus2"
  network_baseline_id = "<network-id>"

  # Both fields are required on every entry, and `name` is used verbatim as the
  # App Gateway SSL certificate name.
  azure_certificates = [
    # Unversioned URI: the platform re-resolves it on every reconcile, so the
    # gateway picks up certificate rotations automatically. Prefer this.
    {
      name                = "wildcard-cert"
      key_vault_secret_id = "https://myvault.vault.azure.net/secrets/wildcard-cert"
    },
    # Versioned URI: pins that exact version forever. Only use it when you
    # deliberately do not want rotations picked up.
    {
      name                = "pinned-cert"
      key_vault_secret_id = "https://myvault.vault.azure.net/secrets/pinned-cert/1234567890abcdef1234567890abcdef"
    },
  ]

  timeouts {
    create = "30m"
    delete = "15m"
  }
}
