# Just-in-time Kubernetes credentials that never reach the plan or state file
# (Terraform 1.10+). Prefer this over the data source of the same name: the
# data source stores the token in state.
ephemeral "duploai_k8s_credentials" "cluster" {
  scope_id = "<kubernetes-scope-id>"
}

# Provider configuration is an ephemeral context, so the token can be passed
# straight in. It is minted per run and valid only for minutes.
provider "kubernetes" {
  host                   = ephemeral.duploai_k8s_credentials.cluster.endpoint
  token                  = ephemeral.duploai_k8s_credentials.cluster.token
  cluster_ca_certificate = base64decode(ephemeral.duploai_k8s_credentials.cluster.ca_certificate_data_base64)
}
