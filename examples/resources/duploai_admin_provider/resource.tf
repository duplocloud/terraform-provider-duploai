# AWS (cloud) provider — IAM role authentication.
resource "duploai_admin_provider" "aws_iam_role" {
  name       = "aws-prod"
  type       = "AWS"
  category   = "cloud"
  account_id = "123456789012"

  credentials = [
    {
      name = "default"
      credential_fields = [
        { key = "iamRoleArn", value = "arn:aws:iam::123456789012:role/DuploCloud-AWS-Admin", type = "string" },
      ]
    }
  ]
}

# AWS (cloud) provider — access key authentication (Access Key ID + secret).
resource "duploai_admin_provider" "aws_access_key" {
  name       = "aws-access-key"
  type       = "AWS"
  category   = "cloud"
  account_id = "123456789012"

  credentials = [
    {
      name = "default"
      credential_fields = [
        { key = "accessKeyId", value = "AKIAIOSFODNN7EXAMPLE", type = "string" },
        { key = "password", value = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", type = "secret", is_sensitive = true },
      ]
    }
  ]
}

# EKS (kubernetes) provider — cloud role to assume.
resource "duploai_admin_provider" "eks" {
  name       = "eks-cluster"
  type       = "eks"
  category   = "kubernetes"
  account_id = "https://XXXX.gr7.us-west-2.eks.amazonaws.com"

  credentials = [
    {
      name = "default"
      credential_fields = [
        { key = "cloudRoleToAssume", value = "arn:aws:iam::123456789012:role/DuploCloud-EKS-Admin", type = "string" },
      ]
    }
  ]
}

# GitHub (source control) provider — API key.
resource "duploai_admin_provider" "github" {
  name       = "github"
  type       = "github"
  category   = "sourceControl"
  account_id = "github.com"

  credentials = [
    {
      name = "default"
      credential_fields = [
        { key = "apikey", value = "ghp_xxx", type = "secret", is_sensitive = true },
      ]
    }
  ]
}

# Observability provider — API-key based (category "observability").
resource "duploai_admin_provider" "observability" {
  name     = "datadog"
  type     = "datadog"
  category = "observability"

  credentials = [
    {
      name = "default"
      credential_fields = [
        { key = "apiKey", value = "dd_api_key", type = "secret", is_sensitive = true },
        { key = "appKey", value = "dd_app_key", type = "secret", is_sensitive = true },
      ]
    }
  ]
}

# Incident management provider (PagerDuty) — category "incidentManagement".
resource "duploai_admin_provider" "incident" {
  name     = "pagerduty"
  type     = "pagerduty"
  category = "incidentManagement"

  credentials = [
    {
      name = "default"
      credential_fields = [
        { key = "apiKey", value = "pd_api_key", type = "secret", is_sensitive = true },
      ]
    }
  ]
}

# Sales provider (Salesforce) — category "sales".
resource "duploai_admin_provider" "sales" {
  name     = "salesforce"
  type     = "salesforce"
  category = "sales"

  credentials = [
    {
      name = "default"
      credential_fields = [
        { key = "apiKey", value = "sf_api_key", type = "secret", is_sensitive = true },
      ]
    }
  ]
}

# Marketing provider (HubSpot) — category "marketing".
resource "duploai_admin_provider" "marketing" {
  name     = "hubspot"
  type     = "hubspot"
  category = "marketing"

  credentials = [
    {
      name = "default"
      credential_fields = [
        { key = "apiKey", value = "hs_api_key", type = "secret", is_sensitive = true },
      ]
    }
  ]
}

# GRC provider (Vanta) — category "grc".
resource "duploai_admin_provider" "grc" {
  name     = "vanta"
  type     = "vanta"
  category = "grc"

  credentials = [
    {
      name = "default"
      credential_fields = [
        { key = "apiKey", value = "grc_api_key", type = "secret", is_sensitive = true },
      ]
    }
  ]
}

# "Other" provider — no credentials, metadata only (category "other").
resource "duploai_admin_provider" "other" {
  name     = "custom-provider"
  type     = "other"
  category = "other"

  metadata = {
    purpose = "example"
  }
}
