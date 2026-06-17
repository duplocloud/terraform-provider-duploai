# Terraform state backend for the services tier.
# Local for now — this tier keeps its own terraform.tfstate in this directory.
terraform {
  backend "local" {}

  # Future: point at a cloud bucket instead. Each tier needs a distinct `key`
  # so they don't share one state object.
  #   backend "s3" {
  #     bucket = "<state-bucket>"
  #     key    = "three-tier-example/services/terraform.tfstate"
  #     region = "us-west-2"
  #   }
}
