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
| [`duploai_admin_ai_agent`](docs/resources/admin_ai_agent.md) | Manages a DuploCloud AI Helpdesk AI Agent — an external AI service or model the help desk can invoke to perform automated tasks. Agents are assigned to personas, which determine where they are available |
| [`duploai_admin_command_policy`](docs/resources/admin_command_policy.md) | Manages a DuploCloud AI Helpdesk command policy — reusable allow/block regular-expression lists that govern which agent-proposed commands are auto-approved or denied (deny-wins). A policy has no effect on its own; it takes effect once bound to a scope (System, Workspace, or Project) by a command policy mapping |
| [`duploai_admin_command_policy_mapping`](docs/resources/admin_command_policy_mapping.md) | Manages a DuploCloud AI Helpdesk command policy mapping — binds a command policy to a scope (System, Workspace, or Project), putting the policy's allow/block command rules into effect for tickets in that scope. Only one active mapping may exist per System scope, and per Workspace/Project target |
| [`duploai_admin_permission_set`](docs/resources/admin_permission_set.md) | Manages a DuploCloud AI Helpdesk permission set — a named grant of workspace-scoped access (scopes and agents) that can be assigned to users via permission set groups |
| [`duploai_admin_persona`](docs/resources/admin_persona.md) | Manages a DuploCloud AI Helpdesk persona — a named assistant profile (prompt and assigned skills) that determines how an AI agent behaves and which skills it can use |
| [`duploai_admin_provider`](docs/resources/admin_provider.md) | Manages a DuploCloud AI Helpdesk Provider — a registered cloud, Kubernetes, source-control, or observability provider (with its authentication credentials) that scopes and agents use |
| [`duploai_admin_quota_definition`](docs/resources/admin_quota_definition.md) | Manages a DuploCloud AI Helpdesk quota definition — a spend/token limit (with a buffer) applied over a daily or monthly period, which quota mappings then bind to platform or workspace scopes |
| [`duploai_admin_quota_mapping`](docs/resources/admin_quota_mapping.md) | Manages a DuploCloud AI Helpdesk quota mapping — binds a quota definition to a scope (platform-wide or specific workspaces) and a dimension (workspace, user, or ticket) |
| [`duploai_admin_scope`](docs/resources/admin_scope.md) | Manages a DuploCloud AI scope: a credentialed view over a provider's resources, filtered by AWS, Kubernetes, and Git rules |
| [`duploai_admin_skill`](docs/resources/admin_skill.md) | Manages a DuploCloud AI Helpdesk skill — a reusable capability (Markdown docs, a package, or a private Git repo) that is assigned to personas |
| [`duploai_admin_user`](docs/resources/admin_user.md) | Manages a DuploCloud AI user account, including identity, roles, and metadata |
| [`duploai_admin_workspace`](docs/resources/admin_workspace.md) | Manages a DuploCloud AI Helpdesk workspace — the top-level container that groups personas, scopes, a quota, and configuration for a team or tenant |
| [`duploai_admin_workspace_scope_mapping`](docs/resources/admin_workspace_scope_mapping.md) | Attaches an infrastructure scope to a DuploCloud AI workspace. Use this to manage the link on its own — for example when the workspace is managed elsewhere, or by a different team — instead of listing the scope in the workspace's `scope_ids`. Manage a given scope with one or the other, never both |
| [`duploai_app_service`](docs/resources/app_service.md) | Manages a DuploCloud AI Helpdesk app service (Kubernetes Deployment) |
| [`duploai_aws_efs`](docs/resources/aws_efs.md) | Manages a DuploCloud AI Helpdesk AWS EFS file system — an elastic, managed NFS file system provisioned within an environment and resource group |
| [`duploai_aws_lambda`](docs/resources/aws_lambda.md) | Manages a DuploCloud AI Helpdesk AWS Lambda function — a serverless compute resource that runs code in response to events within an environment and resource group |
| [`duploai_aws_secret`](docs/resources/aws_secret.md) | Manages a DuploCloud AI Helpdesk AWS Secrets Manager secret |
| [`duploai_azure_key_vault`](docs/resources/azure_key_vault.md) | Manages an Azure Key Vault — a managed store for secrets, keys and certificates, provisioned within an environment and resource group. Platform-provisioned vaults always use Azure RBAC: the resource group's managed identity is granted Key Vault Administrator at resource-group scope, so workloads bound to that group can read secrets with no per-vault permission setup |
| [`duploai_azure_key_vault_secret`](docs/resources/azure_key_vault_secret.md) | Manages a secret inside an Azure Key Vault. Key Vault secrets are append-only: writing a name that already exists adds a new version rather than overwriting the old one, so any change here produces a new version and a new `version` value. The secret's value is write-only — the API never returns it, only its metadata |
| [`duploai_azure_managed_redis`](docs/resources/azure_managed_redis.md) | Manages an Azure Managed Redis (Redis Enterprise) instance, provisioned within an environment and resource group.

Azure models this as two ARM resources and the platform creates both: a `Microsoft.Cache/redisEnterprise` cluster (SKU, TLS, high availability, public network access) and a single `Microsoft.Cache/redisEnterprise/databases` child named `default` (clustering policy, eviction policy, modules, persistence, geo-replication, access-key auth). The attributes below are flattened across both. The database child is created only after the cluster finishes provisioning, so `database_id` and the access-key operations are unavailable for a period after the resource first reports Complete — set `wait_for_database` if you need to depend on them |
| [`duploai_azure_node_pool`](docs/resources/azure_node_pool.md) | Manages a node pool (AKS agent pool) on an Azure Kubernetes Service cluster, provisioned within an environment and resource group.

The pool is created on the AKS cluster linked to the resource group, so that resource group must already have a cluster attached and the cluster must have finished provisioning — the API rejects the create otherwise. Every pool is created as a `User` pool backed by a virtual machine scale set; the cluster's own system pool is not managed here.

The platform stores the pool's configuration and sends the whole of it to Azure on every change, so Terraform sends every field back on update, not just the ones that changed. Nothing in this resource surfaces live Azure state such as the current node count or Kubernetes version: the API deliberately withholds the raw cloud snapshot from its responses, so only `arm_resource_id` and the platform's own status are available. The record's version counter and last-updated timestamp are not exposed either — the API refreshes its cloud snapshot on every read and saves the record as it does, so both change on every GET and would report drift on every plan while meaning nothing |
| [`duploai_azure_postgres_flexible_server`](docs/resources/azure_postgres_flexible_server.md) | Manages an Azure Database for PostgreSQL Flexible Server — a managed PostgreSQL instance provisioned within an environment and resource group, with configurable compute, storage, backup, high availability, authentication, and networking |
| [`duploai_azure_private_endpoint`](docs/resources/azure_private_endpoint.md) | Manages an Azure Private Endpoint, giving a PaaS resource a private IP address inside a subnet so it can be reached without traversing the public internet.

The platform does two things per endpoint: it creates the `Microsoft.Network/privateEndpoints` resource, then attaches a Private DNS zone group so the target's public hostname resolves to the private IP from inside the network. The zone is chosen from `sub_resource_name` — `blob` gives `privatelink.blob.core.windows.net`, `vault` gives `privatelink.vaultcore.azure.net`, and so on. The resource only reports Complete once both steps have succeeded, so a Complete endpoint always has working private DNS. Verified live 2026-08-05 against a storage account's blob service.

Everything about an endpoint is immutable: Azure has no in-place update for one, and the API rejects the attempt outright. Any change here — including tags — replaces the endpoint |
| [`duploai_cluster_attributes`](docs/resources/cluster_attributes.md) | Manages DuploCloud AI Helpdesk cluster attributes — add-ons and components installed onto an existing EKS cluster (autoscaler, ALB controller, EFS, external-dns, and more) |
| [`duploai_cluster_baseline`](docs/resources/cluster_baseline.md) | Manages a DuploCloud AI Helpdesk Kubernetes cluster baseline (control plane, networking, and optional system node group), provisionable across any supported cloud. The target cloud is selected via the `cloud` attribute — AWS (EKS), Azure (AKS), GCP (GKE), or bare Kubernetes (K8S_ONLY) — and defaults to AWS |
| [`duploai_ecr`](docs/resources/ecr.md) | Manages a DuploCloud AI Helpdesk AWS ECR (Elastic Container Registry) repository, provisioned within an environment and resource group |
| [`duploai_elasticache`](docs/resources/elasticache.md) | Manages a DuploCloud AI Helpdesk AWS ElastiCache cluster (Redis, Valkey, or Memcached) |
| [`duploai_environment`](docs/resources/environment.md) | Manages a DuploCloud AI environment within a workspace |
| [`duploai_helm_release`](docs/resources/helm_release.md) | Manages a DuploCloud AI Helpdesk Kubernetes Helm release (Flux HelmRelease) |
| [`duploai_helm_repository`](docs/resources/helm_repository.md) | Manages a DuploCloud AI Helpdesk Kubernetes Helm repository (Flux HelmRepository) |
| [`duploai_k8s_config_map`](docs/resources/k8s_config_map.md) | Manages a DuploCloud AI Helpdesk Kubernetes ConfigMap |
| [`duploai_k8s_cron_job`](docs/resources/k8s_cron_job.md) | Manages a DuploCloud AI Helpdesk Kubernetes CronJob — a scheduled workload that runs jobs on a recurring cron schedule within an environment or resource group |
| [`duploai_k8s_ingress`](docs/resources/k8s_ingress.md) | Manages a DuploCloud AI Helpdesk Kubernetes Ingress — an HTTP/HTTPS routing rule set that exposes services within an environment or resource group |
| [`duploai_k8s_job`](docs/resources/k8s_job.md) | Manages a DuploCloud AI Helpdesk Kubernetes Job — a batch workload that runs one or more containers to completion within an environment or resource group |
| [`duploai_k8s_namespace`](docs/resources/k8s_namespace.md) | Manages a DuploCloud AI Helpdesk Kubernetes Namespace |
| [`duploai_k8s_pvc`](docs/resources/k8s_pvc.md) | Manages a DuploCloud AI Helpdesk Kubernetes PersistentVolumeClaim (PVC) |
| [`duploai_k8s_resource_quota`](docs/resources/k8s_resource_quota.md) | Manages a DuploCloud AI Helpdesk Kubernetes ResourceQuota. A ResourceQuota enforces per-namespace compute and object limits |
| [`duploai_k8s_secret`](docs/resources/k8s_secret.md) | Manages a DuploCloud AI Helpdesk Kubernetes Secret |
| [`duploai_k8s_storage_class`](docs/resources/k8s_storage_class.md) | Manages a DuploCloud AI Helpdesk Kubernetes StorageClass. A StorageClass is a cluster-scoped object that describes how a volume is dynamically provisioned; PersistentVolumeClaims reference it by name |
| [`duploai_mcp_server`](docs/resources/mcp_server.md) | Manages a DuploCloud AI Helpdesk MCP (Model Context Protocol) server registration — a tool/data provider that AI agents can connect to over HTTP, SSE, or a raw configuration |
| [`duploai_msk_kafka`](docs/resources/msk_kafka.md) | Manages a DuploCloud AI Helpdesk AWS MSK (Managed Streaming for Apache Kafka) provisioned cluster, in which you size the brokers, provisioned within an environment and resource group |
| [`duploai_msk_kafka_serverless`](docs/resources/msk_kafka_serverless.md) | Manages a DuploCloud AI Helpdesk AWS MSK (Managed Streaming for Apache Kafka) serverless cluster, in which AWS manages broker capacity automatically. Networking (private subnets and security group) and IAM client authentication are configured server-side |
| [`duploai_native_host`](docs/resources/native_host.md) | Manages a DuploCloud native host — an AWS EC2 instance provisioned inside a workspace environment |
| [`duploai_network_baseline`](docs/resources/network_baseline.md) | Manages a DuploCloud AI Helpdesk network baseline. On AWS this provisions a VPC, subnets, and NAT gateways; on Azure it provisions a virtual network, subnets, and NAT gateways via the nested `azure` block. The target cloud is selected with `cloud` |
| [`duploai_node_group`](docs/resources/node_group.md) | Manages a DuploCloud AI Helpdesk AWS EKS managed node group |
| [`duploai_oci_repository`](docs/resources/oci_repository.md) | Manages a DuploCloud AI Helpdesk Kubernetes OCI repository (Flux OCIRepository) |
| [`duploai_plan`](docs/resources/plan.md) | Manages a DuploCloud AI Helpdesk plan (a region landing zone that collects reusable cloud references: a primary hosted zone, certificates, and AMIs). A plan has no provisioning lifecycle of its own — it is created synchronously and its references are edited directly or by the agent |
| [`duploai_rds_cluster`](docs/resources/rds_cluster.md) | Manages a DuploCloud AI Helpdesk AWS RDS (Aurora) cluster |
| [`duploai_rds_instance`](docs/resources/rds_instance.md) | Manages a DuploCloud AI Helpdesk standalone AWS RDS instance |
| [`duploai_resource_group`](docs/resources/resource_group.md) | Manages a DuploCloud AI Helpdesk resource group (shared security groups, IAM role, KMS key) |
| [`duploai_s3_bucket`](docs/resources/s3_bucket.md) | Manages a DuploCloud AI Helpdesk AWS S3 bucket |
| [`duploai_sns`](docs/resources/sns.md) | Manages a DuploCloud AI Helpdesk AWS SNS (Simple Notification Service) topic, provisioned within an environment and resource group |
| [`duploai_sqs`](docs/resources/sqs.md) | Manages a DuploCloud AI Helpdesk AWS SQS (Simple Queue Service) queue, provisioned within an environment and resource group |
| [`duploai_storage_account`](docs/resources/storage_account.md) | Manages a DuploCloud AI Helpdesk Azure Storage Account, provisioned within an environment and resource group |
<!-- resources-end -->

## Data Sources

<!-- data-sources-start -->
| Data Source | Description |
|---|---|
| [`duploai_admin_ai_agent`](docs/data-sources/admin_ai_agent.md) | Manages a DuploCloud AI Helpdesk AI Agent — an external AI service or model the help desk can invoke to perform automated tasks. Agents are assigned to personas, which determine where they are available |
| [`duploai_admin_command_policy`](docs/data-sources/admin_command_policy.md) | Manages a DuploCloud AI Helpdesk command policy — reusable allow/block regular-expression lists that govern which agent-proposed commands are auto-approved or denied (deny-wins). A policy has no effect on its own; it takes effect once bound to a scope (System, Workspace, or Project) by a command policy mapping |
| [`duploai_admin_command_policy_mapping`](docs/data-sources/admin_command_policy_mapping.md) | Manages a DuploCloud AI Helpdesk command policy mapping — binds a command policy to a scope (System, Workspace, or Project), putting the policy's allow/block command rules into effect for tickets in that scope. Only one active mapping may exist per System scope, and per Workspace/Project target |
| [`duploai_admin_permission_set`](docs/data-sources/admin_permission_set.md) | Manages a DuploCloud AI Helpdesk permission set — a named grant of workspace-scoped access (scopes and agents) that can be assigned to users via permission set groups |
| [`duploai_admin_persona`](docs/data-sources/admin_persona.md) | Manages a DuploCloud AI Helpdesk persona — a named assistant profile (prompt and assigned skills) that determines how an AI agent behaves and which skills it can use |
| [`duploai_admin_provider`](docs/data-sources/admin_provider.md) | Manages a DuploCloud AI Helpdesk Provider — a registered cloud, Kubernetes, source-control, or observability provider (with its authentication credentials) that scopes and agents use |
| [`duploai_admin_quota_definition`](docs/data-sources/admin_quota_definition.md) | Manages a DuploCloud AI Helpdesk quota definition — a spend/token limit (with a buffer) applied over a daily or monthly period, which quota mappings then bind to platform or workspace scopes |
| [`duploai_admin_quota_mapping`](docs/data-sources/admin_quota_mapping.md) | Manages a DuploCloud AI Helpdesk quota mapping — binds a quota definition to a scope (platform-wide or specific workspaces) and a dimension (workspace, user, or ticket) |
| [`duploai_admin_scope`](docs/data-sources/admin_scope.md) | Manages a DuploCloud AI scope: a credentialed view over a provider's resources, filtered by AWS, Kubernetes, and Git rules |
| [`duploai_admin_skill`](docs/data-sources/admin_skill.md) | Manages a DuploCloud AI Helpdesk skill — a reusable capability (Markdown docs, a package, or a private Git repo) that is assigned to personas |
| [`duploai_admin_user`](docs/data-sources/admin_user.md) | Manages a DuploCloud AI user account, including identity, roles, and metadata |
| [`duploai_admin_workspace`](docs/data-sources/admin_workspace.md) | Manages a DuploCloud AI Helpdesk workspace — the top-level container that groups personas, scopes, a quota, and configuration for a team or tenant |
| [`duploai_app_service`](docs/data-sources/app_service.md) | Manages a DuploCloud AI Helpdesk app service (Kubernetes Deployment) |
| [`duploai_aws_efs`](docs/data-sources/aws_efs.md) | Manages a DuploCloud AI Helpdesk AWS EFS file system — an elastic, managed NFS file system provisioned within an environment and resource group |
| [`duploai_aws_lambda`](docs/data-sources/aws_lambda.md) | Manages a DuploCloud AI Helpdesk AWS Lambda function — a serverless compute resource that runs code in response to events within an environment and resource group |
| [`duploai_aws_secret`](docs/data-sources/aws_secret.md) | Manages a DuploCloud AI Helpdesk AWS Secrets Manager secret |
| [`duploai_azure_key_vault`](docs/data-sources/azure_key_vault.md) | Manages an Azure Key Vault — a managed store for secrets, keys and certificates, provisioned within an environment and resource group. Platform-provisioned vaults always use Azure RBAC: the resource group's managed identity is granted Key Vault Administrator at resource-group scope, so workloads bound to that group can read secrets with no per-vault permission setup |
| [`duploai_azure_key_vault_secret`](docs/data-sources/azure_key_vault_secret.md) | Manages a secret inside an Azure Key Vault. Key Vault secrets are append-only: writing a name that already exists adds a new version rather than overwriting the old one, so any change here produces a new version and a new `version` value. The secret's value is write-only — the API never returns it, only its metadata |
| [`duploai_azure_managed_redis`](docs/data-sources/azure_managed_redis.md) | Manages an Azure Managed Redis (Redis Enterprise) instance, provisioned within an environment and resource group.

Azure models this as two ARM resources and the platform creates both: a `Microsoft.Cache/redisEnterprise` cluster (SKU, TLS, high availability, public network access) and a single `Microsoft.Cache/redisEnterprise/databases` child named `default` (clustering policy, eviction policy, modules, persistence, geo-replication, access-key auth). The attributes below are flattened across both. The database child is created only after the cluster finishes provisioning, so `database_id` and the access-key operations are unavailable for a period after the resource first reports Complete — set `wait_for_database` if you need to depend on them |
| [`duploai_azure_node_pool`](docs/data-sources/azure_node_pool.md) | Manages a node pool (AKS agent pool) on an Azure Kubernetes Service cluster, provisioned within an environment and resource group.

The pool is created on the AKS cluster linked to the resource group, so that resource group must already have a cluster attached and the cluster must have finished provisioning — the API rejects the create otherwise. Every pool is created as a `User` pool backed by a virtual machine scale set; the cluster's own system pool is not managed here.

The platform stores the pool's configuration and sends the whole of it to Azure on every change, so Terraform sends every field back on update, not just the ones that changed. Nothing in this resource surfaces live Azure state such as the current node count or Kubernetes version: the API deliberately withholds the raw cloud snapshot from its responses, so only `arm_resource_id` and the platform's own status are available. The record's version counter and last-updated timestamp are not exposed either — the API refreshes its cloud snapshot on every read and saves the record as it does, so both change on every GET and would report drift on every plan while meaning nothing |
| [`duploai_azure_postgres_flexible_server`](docs/data-sources/azure_postgres_flexible_server.md) | Manages an Azure Database for PostgreSQL Flexible Server — a managed PostgreSQL instance provisioned within an environment and resource group, with configurable compute, storage, backup, high availability, authentication, and networking |
| [`duploai_azure_private_endpoint`](docs/data-sources/azure_private_endpoint.md) | Manages an Azure Private Endpoint, giving a PaaS resource a private IP address inside a subnet so it can be reached without traversing the public internet.

The platform does two things per endpoint: it creates the `Microsoft.Network/privateEndpoints` resource, then attaches a Private DNS zone group so the target's public hostname resolves to the private IP from inside the network. The zone is chosen from `sub_resource_name` — `blob` gives `privatelink.blob.core.windows.net`, `vault` gives `privatelink.vaultcore.azure.net`, and so on. The resource only reports Complete once both steps have succeeded, so a Complete endpoint always has working private DNS. Verified live 2026-08-05 against a storage account's blob service.

Everything about an endpoint is immutable: Azure has no in-place update for one, and the API rejects the attempt outright. Any change here — including tags — replaces the endpoint |
| [`duploai_cluster_attributes`](docs/data-sources/cluster_attributes.md) | Manages DuploCloud AI Helpdesk cluster attributes — add-ons and components installed onto an existing EKS cluster (autoscaler, ALB controller, EFS, external-dns, and more) |
| [`duploai_cluster_baseline`](docs/data-sources/cluster_baseline.md) | Manages a DuploCloud AI Helpdesk Kubernetes cluster baseline (control plane, networking, and optional system node group), provisionable across any supported cloud. The target cloud is selected via the `cloud` attribute — AWS (EKS), Azure (AKS), GCP (GKE), or bare Kubernetes (K8S_ONLY) — and defaults to AWS |
| [`duploai_ecr`](docs/data-sources/ecr.md) | Manages a DuploCloud AI Helpdesk AWS ECR (Elastic Container Registry) repository, provisioned within an environment and resource group |
| [`duploai_elasticache`](docs/data-sources/elasticache.md) | Manages a DuploCloud AI Helpdesk AWS ElastiCache cluster (Redis, Valkey, or Memcached) |
| [`duploai_environment`](docs/data-sources/environment.md) | Manages a DuploCloud AI environment within a workspace |
| [`duploai_helm_release`](docs/data-sources/helm_release.md) | Manages a DuploCloud AI Helpdesk Kubernetes Helm release (Flux HelmRelease) |
| [`duploai_helm_repository`](docs/data-sources/helm_repository.md) | Manages a DuploCloud AI Helpdesk Kubernetes Helm repository (Flux HelmRepository) |
| [`duploai_k8s_config_map`](docs/data-sources/k8s_config_map.md) | Manages a DuploCloud AI Helpdesk Kubernetes ConfigMap |
| [`duploai_k8s_credentials`](docs/data-sources/k8s_credentials.md) | Fetches just-in-time Kubernetes credentials for a cluster baseline — the API server endpoint, a short-lived bearer token, and the cluster certificate authority — for use in a `kubernetes`, `helm`, or `kubectl` provider block. One endpoint serves every cloud the platform provisions: EKS, AKS, and registered K8S_ONLY clusters.

The cluster must already exist when the configuration is *planned*, not merely by apply time. Terraform resolves a provider's arguments before planning anything that uses it, so an `id` that is only known after apply fails with `Provider configuration is invalid`. Provision the cluster in a separate root module or a prior apply — see the example.

The token is minted per read and expires within minutes, so this is a data source only. It is re-fetched on every plan and written to state in plain text; `sensitive` redacts CLI output, not state |
| [`duploai_k8s_cron_job`](docs/data-sources/k8s_cron_job.md) | Manages a DuploCloud AI Helpdesk Kubernetes CronJob — a scheduled workload that runs jobs on a recurring cron schedule within an environment or resource group |
| [`duploai_k8s_ingress`](docs/data-sources/k8s_ingress.md) | Manages a DuploCloud AI Helpdesk Kubernetes Ingress — an HTTP/HTTPS routing rule set that exposes services within an environment or resource group |
| [`duploai_k8s_job`](docs/data-sources/k8s_job.md) | Manages a DuploCloud AI Helpdesk Kubernetes Job — a batch workload that runs one or more containers to completion within an environment or resource group |
| [`duploai_k8s_namespace`](docs/data-sources/k8s_namespace.md) | Manages a DuploCloud AI Helpdesk Kubernetes Namespace |
| [`duploai_k8s_pvc`](docs/data-sources/k8s_pvc.md) | Manages a DuploCloud AI Helpdesk Kubernetes PersistentVolumeClaim (PVC) |
| [`duploai_k8s_resource_quota`](docs/data-sources/k8s_resource_quota.md) | Manages a DuploCloud AI Helpdesk Kubernetes ResourceQuota. A ResourceQuota enforces per-namespace compute and object limits |
| [`duploai_k8s_secret`](docs/data-sources/k8s_secret.md) | Manages a DuploCloud AI Helpdesk Kubernetes Secret |
| [`duploai_k8s_storage_class`](docs/data-sources/k8s_storage_class.md) | Manages a DuploCloud AI Helpdesk Kubernetes StorageClass. A StorageClass is a cluster-scoped object that describes how a volume is dynamically provisioned; PersistentVolumeClaims reference it by name |
| [`duploai_mcp_server`](docs/data-sources/mcp_server.md) | Manages a DuploCloud AI Helpdesk MCP (Model Context Protocol) server registration — a tool/data provider that AI agents can connect to over HTTP, SSE, or a raw configuration |
| [`duploai_msk_kafka`](docs/data-sources/msk_kafka.md) | Manages a DuploCloud AI Helpdesk AWS MSK (Managed Streaming for Apache Kafka) provisioned cluster, in which you size the brokers, provisioned within an environment and resource group |
| [`duploai_msk_kafka_serverless`](docs/data-sources/msk_kafka_serverless.md) | Manages a DuploCloud AI Helpdesk AWS MSK (Managed Streaming for Apache Kafka) serverless cluster, in which AWS manages broker capacity automatically. Networking (private subnets and security group) and IAM client authentication are configured server-side |
| [`duploai_native_host`](docs/data-sources/native_host.md) | Manages a DuploCloud native host — an AWS EC2 instance provisioned inside a workspace environment |
| [`duploai_network_baseline`](docs/data-sources/network_baseline.md) | Manages a DuploCloud AI Helpdesk network baseline. On AWS this provisions a VPC, subnets, and NAT gateways; on Azure it provisions a virtual network, subnets, and NAT gateways via the nested `azure` block. The target cloud is selected with `cloud` |
| [`duploai_node_group`](docs/data-sources/node_group.md) | Manages a DuploCloud AI Helpdesk AWS EKS managed node group |
| [`duploai_oci_repository`](docs/data-sources/oci_repository.md) | Manages a DuploCloud AI Helpdesk Kubernetes OCI repository (Flux OCIRepository) |
| [`duploai_plan`](docs/data-sources/plan.md) | Manages a DuploCloud AI Helpdesk plan (a region landing zone that collects reusable cloud references: a primary hosted zone, certificates, and AMIs). A plan has no provisioning lifecycle of its own — it is created synchronously and its references are edited directly or by the agent |
| [`duploai_rds_cluster`](docs/data-sources/rds_cluster.md) | Manages a DuploCloud AI Helpdesk AWS RDS (Aurora) cluster |
| [`duploai_rds_instance`](docs/data-sources/rds_instance.md) | Manages a DuploCloud AI Helpdesk standalone AWS RDS instance |
| [`duploai_resource_group`](docs/data-sources/resource_group.md) | Manages a DuploCloud AI Helpdesk resource group (shared security groups, IAM role, KMS key) |
| [`duploai_s3_bucket`](docs/data-sources/s3_bucket.md) | Manages a DuploCloud AI Helpdesk AWS S3 bucket |
| [`duploai_sns`](docs/data-sources/sns.md) | Manages a DuploCloud AI Helpdesk AWS SNS (Simple Notification Service) topic, provisioned within an environment and resource group |
| [`duploai_sqs`](docs/data-sources/sqs.md) | Manages a DuploCloud AI Helpdesk AWS SQS (Simple Queue Service) queue, provisioned within an environment and resource group |
| [`duploai_storage_account`](docs/data-sources/storage_account.md) | Manages a DuploCloud AI Helpdesk Azure Storage Account, provisioned within an environment and resource group |
<!-- data-sources-end -->

## Release Process

See `RELEASE.md` (gitignored — local only) for the full release procedure.
