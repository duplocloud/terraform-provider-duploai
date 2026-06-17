terraform {
  required_providers {
    duploai = {
      source  = "registry.terraform.io/duplocloud/duploai"
      version = "~> 0.2.0"
    }
  }
}

provider "duploai" {
  duplo_host    = var.duplo_host
  duplo_token   = var.duplo_token
  ssl_no_verify = var.ssl_no_verify
}
