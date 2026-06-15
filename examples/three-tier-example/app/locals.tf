locals {
  environment_id = split("/", duploai_environment.this.id)[1]
  rg_id          = split("/", duploai_resource_group.this.id)[1]
  ns             = duploai_k8s_namespace.this.name
}
