# Just-in-time Kubernetes credentials that never reach the plan or state file
# (Terraform 1.10+). Prefer this over the data source of the same name: the
# data source stores the token in state.
#
# workspace_id (the workspace the scope belongs to, e.g.
# duploai_cluster_baseline.<name>.workspace_id) selects the least-privilege
# user route and is required for non-admin tokens; omit it to use the admin route.
ephemeral "duploai_k8s_credentials" "cluster" {
  workspace_id = "<workspace-id>"
  scope_id     = "<kubernetes-scope-id>"
}

# Provider configuration is an ephemeral context, so the token can be passed
# straight in. It is minted per run and valid only for minutes.
provider "kubernetes" {
  host                   = ephemeral.duploai_k8s_credentials.cluster.endpoint
  token                  = ephemeral.duploai_k8s_credentials.cluster.token
  cluster_ca_certificate = base64decode(ephemeral.duploai_k8s_credentials.cluster.ca_certificate_data_base64)
}
