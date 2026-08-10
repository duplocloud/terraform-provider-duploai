# Just-in-time Kubernetes credentials for an already-provisioned cluster.
#
# The credentials are minted through the cluster's Kubernetes scope, so the
# cluster must exist before this configuration is planned — provision it in a
# separate root module or a prior apply, then reference it here by id.
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

output "api_server" {
  value = data.duploai_k8s_credentials.example.endpoint
}

output "namespace" {
  value = data.duploai_k8s_credentials.example.namespace
}

# Both attributes hold the CA base64-encoded, exactly as the API returns it, so
# decode it wherever a PEM is expected. coalesce prefers the credentials
# response and falls back to the cluster baseline, which is what makes this work
# on AWS (where the credentials endpoint returns the CA empty) and on Azure
# (where it is omitted by design, because AKS API-server certificates can carry
# an extension Go rejects).
output "cluster_ca_certificate" {
  value = base64decode(coalesce(
    data.duploai_k8s_credentials.example.ca_certificate_data_base64,
    data.duploai_cluster_baseline.example.certificate_authority,
  ))
}

# Minted per read and valid only for minutes — consume it in the same run.
output "token" {
  value     = data.duploai_k8s_credentials.example.token
  sensitive = true
}

output "cluster_region" {
  value = data.duploai_cluster_baseline.example.region
}

output "cluster_version" {
  value = data.duploai_cluster_baseline.example.version
}
