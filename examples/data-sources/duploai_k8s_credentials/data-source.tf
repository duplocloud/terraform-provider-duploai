# Just-in-time Kubernetes credentials for an already-provisioned cluster.
#
# Look them up by KUBERNETES SCOPE id, not cluster id — the scope decides which
# API server is reachable, which token is minted, and which namespaces it may
# touch.
#
# The scope must exist before this configuration is planned, so provision the
# cluster in a separate root module or a prior apply.
data "duploai_k8s_credentials" "example" {
  scope_id = "<kubernetes-scope-id>"
}

# In practice, reference the owning cluster rather than hardcoding the id:
#
#   data "duploai_k8s_credentials" "example" {
#     scope_id = data.duploai_cluster_baseline.example.scope_id
#   }
#
# Use scope_id (singular), not scope_ids. A cluster carries both: scope_ids are
# the cloud scopes it runs in, while scope_id is the Kubernetes scope the
# platform creates for it. Passing a cloud scope fails with
# "is not a Kubernetes provider".

output "api_server" {
  value = data.duploai_k8s_credentials.example.endpoint
}

# The first namespace the scope grants access to, per its Kubernetes filter.
output "namespace" {
  value = data.duploai_k8s_credentials.example.namespace
}

# Base64-encoded exactly as the API returns it — decode it wherever a PEM is
# expected, e.g. a kubernetes provider's cluster_ca_certificate. Empty on AKS by
# design; omit cluster_ca_certificate there and let the connection skip CA
# verification.
output "cluster_ca_certificate" {
  value = base64decode(data.duploai_k8s_credentials.example.ca_certificate_data_base64)
}

# Minted per read and valid only for minutes — consume it in the same run.
output "token" {
  value     = data.duploai_k8s_credentials.example.token
  sensitive = true
}
