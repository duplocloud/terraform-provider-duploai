# three-tier-example — demo three-tier app

Deploys the [demo-app](https://github.com/duplocloud/demo-app/tree/main/react-express-mongodb)
(React frontend + Express backend + database) on DuploCloud as **three independent
Terraform root configs**, each with **its own state file**. Tiers chain
`infra → app → services` via `terraform_remote_state` — cross-tier IDs flow
automatically, so there is nothing to hand-enter between tiers.

```
three-tier-example/
├── infra/          # own state — network_baseline, cluster_baseline
├── app/            # own state — reads ../infra/terraform.tfstate
├── services/       # own state — reads ../app/terraform.tfstate
├── config/         # per-tier *.tfvars (git-ignored; copy from *.tfvars.example)
├── scripts/        # plan.sh / apply.sh / destroy.sh — run all tiers in order
└── .env.example    # copy to .env for DuploCloud creds (git-ignored)
```

| Tier | Resources | Maps to assignment |
|---|---|---|
| `infra` | `network_baseline`, `cluster_baseline` | Infra + EKS (Assignment 02) |
| `app` | `plan`, `environment`, `resource_group`, `k8s_namespace`, `node_group` | Tenant + ASG worker nodes (§1, §2) |
| `services` | `rds_instance` (db), `k8s_secret` (db), `app_service` (frontend), `app_service` (backend) | DB, secret, frontend/backend, ingress (§3–§5) |

## Quick start

```bash
# 1. credentials (git-ignored)
cp .env.example .env                               # set duplo_host + duplo_token

# 2. per-tier inputs (git-ignored)
cp config/infra.tfvars.example    config/infra.tfvars
cp config/app.tfvars.example      config/app.tfvars
cp config/services.tfvars.example config/services.tfvars
# …then edit .env and each config/*.tfvars with your values

# 3. drive all three tiers, in order
./scripts/plan.sh         # plan    infra → app → services
./scripts/apply.sh        # apply   infra → app → services   (auto-approved)
./scripts/destroy.sh      # destroy services → app → infra   (reverse order)
```

Each tier keeps its own `terraform.tfstate` in its own directory, so **order
matters** — each tier reads the previous tier's state via `terraform_remote_state`.
With no fallback IDs, apply the tiers in order and destroy dependents before
their dependencies (the default reverse order handles this).

## Where to set values

Each tier's `variables.tf` only **declares** inputs (no defaults, no secrets).
Values come from two git-ignored places:

- **Credentials** → `.env` (copied from `.env.example`): `duplo_host`,
  `duplo_token`, optional `ssl_no_verify`. The scripts source `.env` and pass
  these to the provider as `TF_VAR_*` — never written into a `.tf`/`.tfvars`.
- **Per-tier inputs** → `config/<tier>.tfvars` (copied from `*.tfvars.example`):
  - `config/infra.tfvars` — `workspace_id`, `scope_ids`, `region`, `name_prefix`
    (and optional `eks_version`, default `1.35`).
  - `config/app.tfvars` — same identity inputs (app re-exports them downstream).
  - `config/services.tfvars` — `db_master_password` only. Identity and the
    tenant/namespace ids are read from the app tier's state, not set here.

`config/*.tfvars` is git-ignored (`*.tfvars`); the `.example` templates are
tracked.

## Driver scripts

Thin wrappers around `terraform` that run the three tiers in the right order
with one command (design follows the `tenant-terraform-generator` scripts:
shared `_util.sh` / `_env.sh`, one entrypoint per action).

```
scripts/
├── _util.sh     # shared helpers (sourced, not run): args, paths, tf wrappers
├── _env.sh      # validates creds, exports them as TF_VAR_* (sourced)
├── plan.sh      # terraform plan    infra → app → services
├── apply.sh     # terraform apply   infra → app → services   (auto-approved)
└── destroy.sh   # terraform destroy services → app → infra   (reverse order)
```

**Prerequisites:** `terraform` >= 1.0 on `PATH`; a DuploCloud host + token.
`jq` and (optionally) `duplo-jit` are only needed for the interactive login
fallback. `terraform init -input=false` runs before every action, so a fresh
checkout "just works".

### Credentials

The duploai provider reads `duplo_host` / `duplo_token` / `ssl_no_verify` from
provider config. The scripts take them from the environment and export them as
`TF_VAR_*`, so **no secret is ever written into a `.tf` or committed `.tfvars`**.

The easiest way is a local `.env` (auto-loaded by the scripts, git-ignored):

```bash
cp .env.example .env      # then edit
./scripts/apply.sh        # .env is sourced automatically
```

Or export them yourself instead of using `.env`:

```bash
export duplo_host="https://your-portal.duplocloud.net"
export duplo_token="dahp_xxx..."         # or omit and rely on duplo-jit
export ssl_no_verify=false               # optional, default false
```

If a `.env` exists it is sourced on every run, so its values take effect for
that process — keep one source of truth (a local `.env` for dev, plain env
exports for CI, where no `.env` is committed). `.env` can also carry the
optional knobs (`var_file`, `TF_PARALLELISM`, `AUTO_APPROVE`) — see
[`.env.example`](.env.example) for the full list.

If `duplo_token` is unset and you run a script interactively, it falls back to
`duplo-jit duplo --host "$duplo_host" --interactive`. In non-interactive
(CI/automation) runs, `duplo_token` is **required** or the script exits.

### Var files — precedence

Three ways to feed per-tier inputs, in increasing precedence:

1. **Auto-loaded per-tier file** — if `config/<tier>.tfvars` (or
   `config/<tier>.tfvars.json`) exists, it is passed to that tier automatically.
   This is the normal path; you don't pass any flag.
2. **A global var file applied to every tier** — set `var_file`:
   ```bash
   var_file=/abs/or/rel/path/common.tfvars ./scripts/apply.sh
   ```
3. **Inline / explicit flags** — anything after the tier (or after `--`) is
   passed straight to terraform and wins over the above:
   ```bash
   ./scripts/apply.sh infra -- -var-file=other.tfvars -var=region=us-east-1
   ```

### Configuring terraform flags

Any terraform flag can be appended; it is forwarded verbatim. Use a leading
`--` when a value might look like a tier name.

```bash
./scripts/plan.sh                  -refresh=false
./scripts/plan.sh app              -target=duploai_app_service.frontend
./scripts/apply.sh services        -- -replace=duploai_rds_instance.db
./scripts/apply.sh                 -var=name_prefix=staging
```

Two knobs are exposed via env (standard `TF_LOG`, `TF_VAR_*`, … are honored too):

| Env | Default | Effect |
|---|---|---|
| `TF_PARALLELISM` | `1` | terraform `-parallelism` (low value avoids DuploCloud API timeouts) |
| `AUTO_APPROVE`   | _unset_ | `1` adds `-auto-approve` to **destroy** (apply is always auto-approved) |

### Usage patterns

```bash
# ── plan ──────────────────────────────────────────────────────────────────
./scripts/plan.sh                    # plan all three tiers, in order
./scripts/plan.sh infra              # plan just the infra tier
./scripts/plan.sh app -refresh=false # plan app, extra flag forwarded

# ── apply (auto-approved) ──────────────────────────────────────────────────
./scripts/apply.sh                   # apply infra → app → services
./scripts/apply.sh infra             # apply only infra (run before app/services)
./scripts/apply.sh services -- -target=duploai_k8s_secret.db

# ── destroy (reverse order) ────────────────────────────────────────────────
./scripts/destroy.sh                 # prompts per tier: services → app → infra
AUTO_APPROVE=1 ./scripts/destroy.sh  # non-interactive teardown
./scripts/destroy.sh services        # tear down only the services tier

# ── help ────────────────────────────────────────────────────────────────────
./scripts/plan.sh -h                 # (or apply.sh / destroy.sh) prints usage
```

### Without the scripts

You can run terraform by hand; just supply creds and the per-tier var file, and
apply in order (destroy in reverse):

```bash
export TF_VAR_duplo_host=https://your-portal.duplocloud.net TF_VAR_duplo_token=… TF_VAR_ssl_no_verify=false

cd infra      && terraform init && terraform apply -var-file=../config/infra.tfvars
cd ../app     && terraform init && terraform apply -var-file=../config/app.tfvars
cd ../services && terraform init && terraform apply -var-file=../config/services.tfvars
```

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
