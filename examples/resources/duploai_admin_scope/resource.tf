# A scope over an AWS provider credential, filtered to specific resources.
resource "duploai_admin_scope" "aws" {
  name            = "prod-aws-readonly"
  description     = "Read-only view of production AWS resources"
  provider_id     = "<provider-id>"
  credential_name = "prod-aws"

  aws_resource_search_filter = {
    region               = "us-east-1"
    resource_types_regex = ["AWS::EC2::.*", "AWS::RDS::.*"]
    tags = [
      { key = "env", value = "prod" }
    ]
  }

  metadata = {
    team = "platform"
  }
}

# A scope backed by an MCP server (no credential_name needed).
resource "duploai_admin_scope" "k8s" {
  name          = "cluster-view"
  mcp_server_id = "<mcp-server-id>"
  provider_id   = "<provider-id>"

  kubernetes_filter_config = {
    namespace_regex           = ["^app-.*"]
    namespaced_resource_types = ["deployments", "services"]
    cluster_resource_types    = ["nodes"]
  }
}
