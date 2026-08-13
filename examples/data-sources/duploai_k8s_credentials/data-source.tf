# Just-in-time Kubernetes credentials for an already-provisioned cluster.
#
# The credentials are minted through the cluster's Kubernetes scope, so the
# cluster must exist before this configuration is planned — provision it in a
# separate root module or a prior apply, then reference it here by id.
data "duploai_k8s_credentials" "example" {
  workspace_id = "<workspace-id>"
  id           = "<cluster-baseline-id>"
}

output "api_server" {
  value = data.duploai_k8s_credentials.example.endpoint
}

output "namespace" {
  value = data.duploai_k8s_credentials.example.namespace
}

# Base64-encoded exactly as the API returns it — decode it wherever a PEM is
# expected, e.g. a kubernetes provider's cluster_ca_certificate.
output "cluster_ca_certificate" {
  value = base64decode(data.duploai_k8s_credentials.example.ca_certificate_data_base64)
}

# Minted per read and valid only for minutes — consume it in the same run.
output "token" {
  value     = data.duploai_k8s_credentials.example.token
  sensitive = true
}
