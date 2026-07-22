locals {
  environment_id = duploai_environment.this.environment_id
  rg_id          = duploai_resource_group.this.resource_group_id
  ns             = duploai_k8s_namespace.this.name
}
