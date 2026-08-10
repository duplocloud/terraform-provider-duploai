# Just-in-time Kubernetes credentials for an already-provisioned cluster.
data "duploai_k8s_credentials" "example" {
  workspace_id = "<workspace-id>"
  id           = "<cluster-baseline-id>"
}

# The same cluster, for what the credentials response does not carry: region,
# Kubernetes version, and — on AWS today — the certificate authority.
data "duploai_cluster_baseline" "example" {
  workspace_id = "<workspace-id>"
  id           = "<cluster-baseline-id>"
}

locals {
  # Both attributes hold the CA base64-encoded, exactly as the API returns it.
  # coalesce prefers the credentials response and falls back to the cluster
  # baseline, which is what makes this work on AWS (where the credentials
  # endpoint returns the CA empty) and on Azure (where it is omitted by design,
  # because AKS API-server certificates can carry an extension Go rejects).
  # If a cluster somehow had no CA from either source, coalesce fails the plan
  # rather than silently configuring an unverifiable connection.
  cluster_ca = base64decode(coalesce(
    data.duploai_k8s_credentials.example.ca_certificate_data_base64,
    data.duploai_cluster_baseline.example.certificate_authority,
  ))
}

# Configuring a provider from these credentials is what forces the cluster to
# exist before this configuration is planned: Terraform resolves provider
# arguments up front, so an id that is only known after apply (for example a
# duploai_cluster_baseline created in this same run) fails with "Provider
# configuration is invalid". Provision the cluster in a separate root module or
# a prior apply, then reference it here by id.
#
# The token is minted per read and expires within minutes, so keep the data
# source and the provider in one configuration — never pass it between applies.
provider "kubernetes" {
  host                   = data.duploai_k8s_credentials.example.endpoint
  token                  = data.duploai_k8s_credentials.example.token
  cluster_ca_certificate = local.cluster_ca
}

# helm v3 takes `kubernetes` as an attribute (`=`); on v2.x it is a nested block.
provider "helm" {
  kubernetes = {
    host                   = data.duploai_k8s_credentials.example.endpoint
    token                  = data.duploai_k8s_credentials.example.token
    cluster_ca_certificate = local.cluster_ca
  }
}

output "cluster_region" {
  value = data.duploai_cluster_baseline.example.region
}

output "cluster_version" {
  value = data.duploai_cluster_baseline.example.version
}
