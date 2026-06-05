# terraform-provider-duploai

Terraform provider for the DuploCloud AI Helpdesk platform.

## Requirements

- Terraform >= 1.0
- Go >= 1.25 (to build from source)

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
    duploai = {
      source  = "registry.terraform.io/duplocloud/duploai"
      version = "~> 0.0"
    }
  }
}

provider "duploai" {
  duplo_host  = "https://<helpdesk-host>"
  duplo_token = var.duplo_token
}
```

## Environment Variables

| Variable | Description |
|---|---|
| `DUPLO_HOST` | DuploCloud AI Helpdesk base URL |
| `DUPLO_TOKEN` | API bearer token |

## Resources

| Resource | Description |
|---|---|
| [`duploai_network_baseline`](docs/resources/network_baseline.md) | Manages a network baseline (VPC + subnets) |

## Release Process

See `RELEASE.md` (gitignored — local only) for the full release procedure.
