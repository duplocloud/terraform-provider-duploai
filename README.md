# terraform-provider-duplocloud-helpdesk

Terraform provider for the DuploCloud AI Helpdesk platform.

## Requirements

- Terraform >= 1.0
- Go >= 1.21 (to build from source)

## Build & Install (local development)

```bash
make install       # build + install to ~/.terraform.d/plugins/
make test          # unit tests
make testacc       # acceptance tests (requires TF_ACC=1)
make doc           # regenerate docs
```

## Usage

```hcl
terraform {
  required_providers {
    duplocloud = {
      source  = "registry.terraform.io/duplocloud/duplocloud-helpdesk"
      version = "~> 0.1"
    }
  }
}

provider "duplocloud" {
  duplo_host  = "http://localhost:60021"
  duplo_token = var.duplo_token
}
```

## Environment Variables

| Variable | Description |
|---|---|
| `DUPLO_HOST` | DuploCloud AI Helpdesk base URL |
| `DUPLO_TOKEN` | API bearer token |

## Release Process

See `RELEASE.md` (gitignored — local only) for the full release procedure.
