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

<!-- resources-start -->
| Resource | Description |
|---|---|
| [`duploai_app_service`](docs/resources/app_service.md) | Manages a DuploCloud AI Helpdesk app service (Kubernetes Deployment) |
| [`duploai_aws_lambda`](docs/resources/aws_lambda.md) | Manages a DuploCloud AI Helpdesk AWS Lambda function — a serverless compute resource that runs code in response to events within an environment and resource group |
| [`duploai_cluster_attributes`](docs/resources/cluster_attributes.md) | Manages DuploCloud AI Helpdesk cluster attributes — add-ons and components installed onto an existing EKS cluster (autoscaler, ALB controller, EFS, external-dns, and more) |
| [`duploai_cluster_baseline`](docs/resources/cluster_baseline.md) | Manages a DuploCloud AI Helpdesk EKS cluster baseline (control plane, networking, and optional system node group) |
| [`duploai_environment`](docs/resources/environment.md) | Manages a DuploCloud AI environment within a workspace |
| [`duploai_helm_release`](docs/resources/helm_release.md) | Manages a DuploCloud AI Helpdesk Kubernetes Helm release (Flux HelmRelease) |
| [`duploai_helm_repository`](docs/resources/helm_repository.md) | Manages a DuploCloud AI Helpdesk Kubernetes Helm repository (Flux HelmRepository) |
| [`duploai_k8s_config_map`](docs/resources/k8s_config_map.md) | Manages a DuploCloud AI Helpdesk Kubernetes ConfigMap |
| [`duploai_k8s_job`](docs/resources/k8s_job.md) | Manages a DuploCloud AI Helpdesk Kubernetes Job — a batch workload that runs one or more containers to completion within an environment or resource group |
| [`duploai_k8s_namespace`](docs/resources/k8s_namespace.md) | Manages a DuploCloud AI Helpdesk Kubernetes Namespace |
| [`duploai_k8s_pvc`](docs/resources/k8s_pvc.md) | Manages a DuploCloud AI Helpdesk Kubernetes PersistentVolumeClaim (PVC) |
| [`duploai_k8s_secret`](docs/resources/k8s_secret.md) | Manages a DuploCloud AI Helpdesk Kubernetes Secret |
| [`duploai_network_baseline`](docs/resources/network_baseline.md) | Manages a DuploCloud AI Helpdesk network baseline (VPC, subnets, NAT gateways) |
| [`duploai_node_group`](docs/resources/node_group.md) | Manages a DuploCloud AI Helpdesk AWS EKS managed node group |
| [`duploai_oci_repository`](docs/resources/oci_repository.md) | Manages a DuploCloud AI Helpdesk Kubernetes OCI repository (Flux OCIRepository) |
| [`duploai_plan`](docs/resources/plan.md) | Manages a DuploCloud AI Helpdesk plan (region landing zone built on a network baseline) |
| [`duploai_rds_cluster`](docs/resources/rds_cluster.md) | Manages a DuploCloud AI Helpdesk AWS RDS (Aurora) cluster |
| [`duploai_rds_instance`](docs/resources/rds_instance.md) | Manages a DuploCloud AI Helpdesk standalone AWS RDS instance |
| [`duploai_resource_group`](docs/resources/resource_group.md) | Manages a DuploCloud AI Helpdesk resource group (shared security groups, IAM role, KMS key) |
<!-- resources-end -->

## Release Process

See `RELEASE.md` (gitignored — local only) for the full release procedure.
