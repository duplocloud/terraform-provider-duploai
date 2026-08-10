# Fetch just-in-time Kubernetes credentials for a cluster baseline.
# The id is the cluster baseline's object id. The cluster must already be
# provisioned and available — its Kubernetes scope is what mints the token.
data "duploai_k8s_credentials" "example" {
  workspace_id = "<workspace-id>"
  id           = "<cluster-baseline-id>"
}

# The same cluster, for the values the credentials response does not carry:
# the cluster's region and Kubernetes version, and — for AWS-provisioned
# clusters — the certificate authority, which the credentials endpoint
# currently returns empty.
data "duploai_cluster_baseline" "example" {
  workspace_id = "<workspace-id>"
  id           = "<cluster-baseline-id>"
}

# Both attributes hold the certificate authority base64-encoded, exactly as the
# API returns it, so decode at the point of use — `cluster_ca_certificate`
# expects PEM.
locals {
  cluster_ca = base64decode(
    coalesce(
      data.duploai_k8s_credentials.example.ca_certificate_data_base64,
      data.duploai_cluster_baseline.example.certificate_authority,
    )
  )
}

# Configure the Kubernetes provider from the fetched credentials. The token is
# minted per read and expires within minutes, so keep the data source and the
# provider block in the same configuration — never pass the token between applies.
provider "kubernetes" {
  host                   = data.duploai_k8s_credentials.example.endpoint
  token                  = data.duploai_k8s_credentials.example.token
  cluster_ca_certificate = local.cluster_ca
}

# helm provider v3 takes `kubernetes` as an attribute (`=`); on v2.x it is a
# nested block, written without the `=`.
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
