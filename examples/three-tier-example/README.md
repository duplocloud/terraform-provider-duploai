# three-tier-example — demo three-tier app

Deploys the [demo-app](https://github.com/duplocloud/demo-app/tree/main/react-express-mongodb)
(React frontend + Express backend + database) on DuploCloud as **three independent
Terraform root configs**, each with **its own state file**. Tiers chain
`infra → app → services` via `terraform_remote_state` — cross-tier IDs flow
automatically, so there is nothing to hand-enter between tiers.

```
three-tier-example/
├── infra/      # own state — network_baseline, cluster_baseline
├── app/        # own state — reads ../infra/terraform.tfstate
└── services/   # own state — reads ../app/terraform.tfstate
```

| Tier | Resources | Maps to assignment |
|---|---|---|
| `infra` | `network_baseline`, `cluster_baseline` | Infra + EKS (Assignment 02) |
| `app` | `plan`, `environment`, `resource_group`, `k8s_namespace`, `node_group` | Tenant + ASG worker nodes (§1, §2) |
| `services` | `rds_instance` (db), `k8s_secret` (db), `app_service` (frontend), `app_service` (backend) | DB, secret, frontend/backend, ingress (§3–§5) |

## Where to set values

There is **no shared/global variables file**. Each tier owns its `variables.tf`
and you edit values only in that tier:

- `infra/variables.tf` — `workspace_id`, `scope_ids`, `region`, `name_prefix`, provider creds.
- `app/variables.tf` — same identity inputs + provider creds (app is the tier that
  re-exports identity downstream).
- `services/variables.tf` — provider creds + `db_master_password` only. Identity and the
  tenant/namespace ids are read from the app tier's state, not set here.

## Assignment → implemented resource

| Assignment item | Resource used | Notes |
|---|---|---|
| Tenant `<name>-ass30` | `resource_group` | direct |
| ASG worker nodes (t3a.medium, 1/1/2) | `node_group` | direct |
| MongoDB / DocumentDB `app` | `rds_instance` **engine=mysql** | ⚠️ **substitution** — DocumentDB isn't an implemented resource; the DB tier uses MySQL on `rds_instance` (db.t3.medium) |
| K8s secret `db` | `k8s_secret` | Opaque, holds DB connection details |
| Frontend service | `app_service` (frontend) | container + ClusterIP service (port 3000) |
| Backend service (`envFrom` secret) | `app_service` (backend) | ⚠️ `app_service` has **no `envFrom`/secretRef** — DB creds passed as inline `env` (mirrors the `db` secret) |
| ALB Ingress (host rules) | `app_service.ingress` (on frontend) | one ALB ingress, host-based rules for both services |

## Known gaps / substitutions
- **No DocumentDB resource** → MySQL `rds_instance` stands in (not Mongo-compatible; the demo app would need a real DocDB resource).
- **No `envFrom`/secretRef on `app_service`** → backend DB creds are inline `env`; the `k8s_secret` is still created to represent the requirement.
- **`rds_instance` exposes no endpoint output** → the DB host in the secret/backend env is a `<db-endpoint>` placeholder; fill it from the provisioned DB endpoint (console) after the DB is `available`.

## Usage

Apply the tiers **in order** — each reads the previous tier's state:

```bash
(cd .. && make install)        # build + install the provider once

cd infra    && terraform init && terraform apply   # 1. network + cluster
cd ../app   && terraform init && terraform apply   # 2. tenant, namespace, nodes
cd ../services && terraform init && terraform apply # 3. db, secret, frontend, backend
```

Each tier keeps its own `terraform.tfstate` in its own directory. Destroy in reverse
order (`services` → `app` → `infra`).
