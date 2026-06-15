# Terraform state backend for the infra tier.
# Local for now — this tier keeps its own terraform.tfstate in this directory,
# which the downstream tier reads via terraform_remote_state.
terraform {
  backend "local" {}

  # Future: point at a cloud bucket instead. Each tier needs a distinct `key`
  # so they don't share one state object.
  #   backend "s3" {
  #     bucket = "<state-bucket>"
  #     key    = "three-tier-example/infra/terraform.tfstate"
  #     region = "us-west-2"
  #   }
}
