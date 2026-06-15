# Pulls app outputs (tenant, namespace, identity) from the app tier's state file.
# Run `terraform apply` in ../app first.
data "terraform_remote_state" "app" {
  backend = "local"
  config = {
    path = "../app/terraform.tfstate"
  }
}

locals {
  # Identity and tenant/namespace ids flow in from the app tier's state via the
  # data source above — nothing is hand-entered in this tier.
  _app = data.terraform_remote_state.app.outputs

  workspace_id      = local._app.workspace_id
  scope_ids         = local._app.scope_ids
  name_prefix       = local._app.name_prefix
  environment_id    = local._app.environment_id
  resource_group_id = local._app.resource_group_id
  namespace_name    = local._app.namespace_name
}
