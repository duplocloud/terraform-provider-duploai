# Pulls infra outputs from the infra tier's own state file.
# Run `terraform apply` in ../infra first.
data "terraform_remote_state" "infra" {
  backend = "local"
  config = {
    path = "../infra/terraform.tfstate"
  }
}

locals {
  network_id = data.terraform_remote_state.infra.outputs.network_id
  cluster_id = data.terraform_remote_state.infra.outputs.cluster_id
}
