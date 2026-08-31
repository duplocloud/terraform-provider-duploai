# Basic environment — provisions new infrastructure (Create mode)
resource "duploai_environment" "basic" {
  workspace_id = "<workspace-id>"
  name         = "prod-env"
  scope_ids    = ["<scope-id>"]
}

# Environment with an explicit provisioner and a longer create timeout
resource "duploai_environment" "full" {
  workspace_id = "<workspace-id>"
  name         = "prod-env-iac"
  scope_ids    = ["<scope-id>"]
  description  = "Production environment managed via IaC"

  mode                = "Create"
  provisioner_type    = "IacNativeTf"
  provisioner_version = "1.0.0"
  plan_ids            = ["<plan-id>"]

  # Free-form key/value metadata stored on the environment record. Provide the
  # complete map — on update it replaces the previous one.
  metadata = {
    owner       = "platform-team"
    cost-center = "cc-4417"
  }

  timeouts {
    create = "45m"
    update = "30m"
    delete = "15m"
  }
}
