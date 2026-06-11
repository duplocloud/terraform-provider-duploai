# Contributing to terraform-provider-duploai

This guide covers everything needed to add a new resource, fix a bug, and get a PR merged.

---

## Table of Contents

1. [Development setup](#1-development-setup)
2. [Architecture overview](#2-architecture-overview)
3. [Adding a new resource](#3-adding-a-new-resource)
4. [Spec reference](#4-spec-reference)
5. [Endpoint reference](#5-endpoint-reference)
6. [Waiter reference](#6-waiter-reference)
7. [Common patterns](#7-common-patterns)
8. [Writing examples and generating docs](#8-writing-examples-and-generating-docs)
9. [Testing](#9-testing)
10. [Self-reviewing before raising a PR](#10-self-reviewing-before-raising-a-pr)
11. [Raising a PR](#11-raising-a-pr)

---

## 1. Development setup

### Prerequisites

- Go ≥ 1.21
- Terraform ≥ 1.0
- `golangci-lint` (for linting)

### Build and install locally

```bash
make install       # build + install to ~/.terraform.d/plugins/
make test          # unit tests
make doc           # regenerate docs + update README resources table
make vet           # vet + lint
```

After `make install`, create a `.terraformrc` file that points Terraform at your local build:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/duplocloud/duploai" = "/Users/<you>/.terraform.d/plugins/registry.terraform.io/duplocloud/duploai/0.0.1/<os>_<arch>"
  }
  direct {}
}
```

Then set `TF_CLI_CONFIG_FILE=../.terraformrc` when running `terraform` commands in a test directory.

---

## 2. Architecture overview

This provider is **data-driven**. Every resource is defined by exactly two files:

| File | Location | Defines |
|------|----------|---------|
| `<name>.json` | `duplocloud/specs/` | Terraform schema, API field mapping, waiter |
| `<name>.go` *(optional)* | `duplosdk/` | API URIs and HTTP verbs |

A generic engine (`duplocloud/spec.go`, `dynamic_resource.go`, `typesystem.go`) reads the spec at startup and generates the full Terraform resource — schema, Create/Read/Update/Delete, import, plan modifiers, and async waiting — without any per-resource Go code.

**You never write CRUD logic.** To add a resource you only need to:

1. Describe its schema in a JSON spec.
2. Register its API endpoint (either inline in the spec or in a small Go file).

The spec `"name"` field determines the Terraform resource type: `"name": "network_baseline"` → `duploai_network_baseline`.

### Inline vs Go endpoint

The endpoint can be declared two ways:

**Inline** (preferred for standard REST resources — no Go file needed):

```json
{
  "name": "my_resource",
  "endpoint": {
    "uriBase": "/v1/.../workspaces/{workspace_id}/environment/MyResources",
    "deprovision": {}
  }
}
```

**Go file** (use when the endpoint needs custom logic or already exists):

```go
// duplosdk/my_resource.go
package duplosdk
import "net/http"
func init() {
    RegisterEndpoint("my_resource", duplosdk.Endpoint{
        UriBase:     "/v1/.../workspaces/{workspace_id}/environment/MyResources",
        Create:      Operation{Verb: http.MethodPost, Path: ""},
        Read:        Operation{Verb: http.MethodGet, Path: "/{id}"},
        Update:      Operation{Verb: http.MethodPut, Path: "/{id}"},
        Delete:      Operation{Verb: http.MethodDelete, Path: "/{id}"},
        Deprovision: Operation{Verb: http.MethodPost, Path: "/{id}/deprovision"},
    })
}
```

---

## 3. Adding a new resource

### Step 1 — Understand the API

Before writing the spec, answer these questions from the API (Swagger or backend source):

- What is the base URL? Which path params scope the resource (e.g. `{workspace_id}`)?
- What does the **POST** (create) body look like? What fields are required?
- What does the **GET** (read) response look like? Which response paths differ from request paths?
- Does **PUT** (update) exist? Does it accept the same body as POST or a different one?
- Is **DELETE** immediate, or does the resource require a deprovision step first?
- Which fields are server-set / computed (never sent by the client)?
- Which fields are immutable after creation (changing them requires destroy+recreate)?

### Step 2 — Create the spec

Copy `examples/adding-a-resource/foo.json` into `duplocloud/specs/<name>.json` and edit it. Key rules:

- The file name (minus `.json`) and `"name"` must match.
- Path-parameter attributes (like `workspace_id`) must match `{placeholder}` names in `uriBase`, must be `required + forceNew`, and must have **no** `apiPath` (they go into the URL, not the body).
- Every body field needs an explicit `apiPath`, even if it's just `"name"`.
- Mark server-set outputs as `computed: true` + `noSend: true`.
- Mark identity-binding fields as `forceNew: true` (changing them requires replacement).

### Step 3 — Register the endpoint

If you used the inline `"endpoint"` block in the spec, skip this step — no Go file is needed.

Otherwise, create `duplosdk/<name>.go`:

```go
package duplosdk
import "net/http"
func init() {
    RegisterEndpoint("<name>", Endpoint{
        UriBase: "/v1/.../workspaces/{workspace_id}/environment/Resources",
        Create:  Operation{Verb: http.MethodPost, Path: ""},
        Read:    Operation{Verb: http.MethodGet, Path: "/{id}"},
        Update:  Operation{Verb: http.MethodPut, Path: "/{id}"},
        Delete:  Operation{Verb: http.MethodDelete, Path: "/{id}"},
    })
}
```

### Step 4 — Build and test

```bash
go build ./...
go test ./duplocloud/ -run TestDynamicResource
```

The engine validates the spec at load time. A malformed spec (unknown type, missing `apiPath` on a body field, `{placeholder}` with no matching attribute) causes a loud startup panic rather than a silent runtime error.

### Step 5 — Write examples

Create `examples/resources/duploai_<name>/resource.tf` with one or more realistic examples:

```hcl
# Basic example
resource "duploai_my_resource" "example" {
  workspace_id = "ws-abc123"
  name         = "my-resource"
  region       = "us-east-1"
}
```

Also add `examples/resources/duploai_<name>/import.sh`:

```bash
# Import an existing resource.
#  - WORKSPACE_ID is the ID of the workspace
#  - RESOURCE_ID   is the ID of the resource
terraform import duploai_my_resource.example WORKSPACE_ID/RESOURCE_ID
```

### Step 6 — Generate docs and update README

```bash
make doc
```

This runs three steps automatically:

1. `terraform fmt -recursive ./examples/` — formats all example `.tf` files.
2. `tfplugindocs generate` — writes `docs/resources/<name>.md` from the schema + examples.
3. `go run ./tools/gen_readme` — updates the Resources table in `README.md`.

Commit all generated files (`docs/`, `README.md`) in the same PR.

---

## 4. Spec reference

### Top-level fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Resource suffix → `duploai_<name>`. Must match the file name and the endpoint key. |
| `description` | string | Human-readable description. Never expose backend storage internals (no "MongoDB", "ObjectId"). |
| `idPath` | string | Dot-path to the object id in the create/read response (e.g. `id`). |
| `endpoint` | object | Inline endpoint spec (see §5). Omit if you have a `duplosdk/<name>.go` file. |
| `attributes` | array | Schema attributes (see below). |
| `requestConstants` | array | `{path, value}` fields injected into **every** request body (POST and PUT). Use for fixed envelope fields the user never sets. |
| `createConstants` | array | Like `requestConstants` but injected on **POST only**. Use for creation-only discriminators (e.g. `spec.mode = "Create"`). |
| `updateConstants` | array | Like `requestConstants` but injected on **PUT only**. |
| `requiredIf` | array | `{attribute, whenAttribute, whenEquals}` — conditional required validation at plan time. |
| `waiter` | object | Async poller for provisioned resources (see §6). |

### Attribute fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Attribute name in snake_case. |
| `type` | string | One of: `string`, `bool`, `int`, `number`, `list(T)`, `set(T)`, `map(T)`, `object`, `list(object)`, `set(object)`, `map(object)`. T is any primitive. |
| `description` | string | Shown in docs and `terraform schema`. |
| `required` | bool | Must be set by the user. |
| `optional` | bool | May be set by the user. |
| `computed` | bool | Value may be set by the server. Use with `optional` for optional+computed (server can default). |
| `sensitive` | bool | Redact in plan/state output. |
| `forceNew` | bool | Changing this attribute requires destroy+recreate (RequiresReplace). |
| `default` | any | Static default value (only with `optional+computed`). JSON literal of the correct type. |
| `oneOf` | array | Enum constraint for string attributes. |
| `apiPath` | string | Dot-path in the API body, both directions (e.g. `spec.region`). Empty = path parameter (for top-level) or attribute name (for nested). |
| `requestPath` | string | Overrides `apiPath` for the **request** (POST/PUT) body only. |
| `responsePath` | string | Overrides `apiPath` for the **response** (GET) body only. |
| `createPath` | string | Overrides `requestPath`/`apiPath` for **POST** only. Use when create and update send to different body paths. |
| `updatePath` | string | Overrides `requestPath`/`apiPath` for **PUT** only. |
| `createOnly` | bool | Field is sent on POST but skipped entirely on PUT. Use for fields that cannot be changed after creation (e.g. S3 bucket/key for Lambda code). |
| `noSend` | bool | Mapped from the response only; never sent in the request. Use for computed-only outputs (`status`, ARNs). |
| `attributes` | array | Nested attributes for object types. Nest to any depth. |

### Path syntax

- `a.b.c` — nested object field.
- `a.b[].c` — map over an array, extracting `c` from each element (read back only).
- `{placeholder}` in `uriBase` — matched to a top-level `required + forceNew` attribute with no `apiPath`.

### `forceNew` and `computed` interaction

| Combination | Engine behavior |
|-------------|-----------------|
| `computed` only | Recomputes each apply (status fields, ARNs). `UseStateForUnknown` is NOT attached. |
| `optional + computed` | `UseStateForUnknown` attached — preserves state value when unchanged. |
| `computed + forceNew` | `UseStateForUnknown` attached — inherited values are quiet (no "known after apply" churn). |

---

## 5. Endpoint reference

### Inline endpoint (in the spec JSON)

```json
"endpoint": {
  "uriBase": "/v1/.../workspaces/{workspace_id}/environment/Resources",
  "immutable": false,
  "create":      { "verb": "POST", "path": "" },
  "read":        { "verb": "GET",  "path": "/{id}" },
  "update":      { "verb": "PUT",  "path": "/{id}" },
  "delete":      { "verb": "DELETE", "path": "/{id}" },
  "deprovision": { "verb": "POST", "path": "/{id}/deprovision" }
}
```

All operation fields are optional — defaults are `POST ""`, `GET /{id}`, `PUT /{id}`, `DELETE /{id}`. The engine auto-adds `Update: PUT /{id}` unless `"immutable": true` is set.

Using `"deprovision": {}` (empty object) is shorthand for the default `POST /{id}/deprovision`.

### Go endpoint file (`duplosdk/<name>.go`)

Use this when the inline spec is insufficient (e.g. the resource already has a Go SDK file, or needs non-standard paths).

```go
package duplosdk
import "net/http"
func init() {
    RegisterEndpoint("<name>", Endpoint{
        UriBase:     "/v1/.../workspaces/{workspace_id}/environment/Resources",
        Create:      Operation{Verb: http.MethodPost,   Path: ""},
        Read:        Operation{Verb: http.MethodGet,    Path: "/{id}"},
        Update:      Operation{Verb: http.MethodPut,    Path: "/{id}"},
        Delete:      Operation{Verb: http.MethodDelete, Path: "/{id}"},
        // Optional — required when the API refuses to delete a live resource.
        Deprovision: Operation{Verb: http.MethodPost,   Path: "/{id}/deprovision"},
    })
}
```

**Immutable resources** (no update, all changes force replacement) — omit `Update` and add `forceNew: true` to all input attributes:

```go
RegisterEndpoint("k8s_job", Endpoint{
    UriBase: base,
    Create:  Operation{Verb: http.MethodPost,   Path: ""},
    Read:    Operation{Verb: http.MethodGet,    Path: "/{id}"},
    Delete:  Operation{Verb: http.MethodDelete, Path: "/{id}"},
    // No Update: Kubernetes Jobs are immutable after creation.
})
```

---

## 6. Waiter reference

Add a `waiter` block for resources that are provisioned asynchronously (the API returns immediately but the underlying cloud resource takes time to become ready).

```json
"waiter": {
  "statusPath": "status",
  "successState": "Complete",
  "failureStates": {
    "Failed":             "provisioning failed",
    "Blocked":            "provisioning is blocked",
    "WaitingForApproval": "provisioning is waiting for manual approval, which Terraform cannot provide",
    "DeprovisionFailed":  "deprovisioning failed"
  },
  "failureDetailPath":         "blockedReason",
  "deprovisionedState":        "DeProvisioned",
  "failureRetries":             3,
  "failurePollIntervalSeconds": 60,
  "pollIntervalSeconds":        15,
  "createTimeoutMinutes":       60,
  "updateTimeoutMinutes":       60,
  "deleteTimeoutMinutes":       30
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `statusPath` | `"status"` | Dot-path to the status string in the read response. |
| `successState` | `"Complete"` | Terminal success value — the waiter stops and the operation succeeds. |
| `failureStates` | see below | Map of terminal failure values → human-readable reasons. The waiter stops and returns an error. |
| `failureDetailPath` | `"blockedReason"` | Optional path to extra error context appended to the failure message. |
| `deprovisionedState` | *(none)* | Required when the endpoint has a `Deprovision` op. The delete flow waits for this state before issuing the final delete. |
| `failureRetries` | `0` | Extra polls to tolerate after first seeing a failure state. Use for backends that transiently report failure before self-recovering. |
| `failurePollIntervalSeconds` | *(falls back to `pollIntervalSeconds`)* | Longer poll interval used during failure retries — gives the backend more time to recover between polls. |
| `pollIntervalSeconds` | `10` | Polling cadence during normal operation. |
| `createTimeoutMinutes` | `30` | Default create timeout (overridable via `timeouts {}` block in HCL). |
| `updateTimeoutMinutes` | `30` | Default update timeout. |
| `deleteTimeoutMinutes` | `15` | Default delete timeout. |

The defaults for `statusPath`, `successState`, `failureStates`, and `failureDetailPath` are the DuploCloud AI Helpdesk platform defaults — a spec's `waiter` block only needs to set fields that differ. Typically you only need to set `deprovisionedState` and any timeout/interval overrides.

**Users can override `failure_retries`** at the resource level in HCL:

```hcl
resource "duploai_cluster_attributes" "example" {
  # ...
  failure_retries = 5
}
```

---

## 7. Common patterns

### Deprovision-before-delete

Most DuploCloud AI resources cannot be deleted while still live — the API returns an error. These resources require a two-step delete: trigger teardown (deprovision), wait for it to complete, then delete the record.

Add a `Deprovision` operation and set `waiter.deprovisionedState`:

```json
"endpoint": {
  "uriBase": "...",
  "deprovision": {}
},
"waiter": {
  "deprovisionedState": "DeProvisioned"
}
```

The engine runs the full sequence automatically: `POST /{id}/deprovision` → poll until `"DeProvisioned"` → `DELETE /{id}`.

### Split request/response paths (`createPath` / `updatePath`)

Some APIs use different request body structures for POST vs PUT:

```json
{ "name": "memory_size",
  "type": "int",
  "optional": true,
  "computed": true,
  "createPath": "spec.createRequest.memorySize",
  "updatePath": "spec.updateRequest.memorySize",
  "responsePath": "result.cloudDetails.configuration.memorySize" }
```

### Create-only fields (`createOnly`)

Fields that are sent on POST but cannot be changed after creation (code source fields, initial tags):

```json
{ "name": "s3_bucket",
  "type": "string",
  "optional": true,
  "createOnly": true,
  "requestPath": "spec.createRequest.code.s3Bucket" }
```

On PUT these fields are silently omitted. Combine with `forceNew: true` if the field is an immutable identity field that should force replacement when changed.

### Verb-scoped constants

Use `createConstants` / `updateConstants` when a fixed discriminator field should only go on POST or PUT:

```json
"createConstants": [
  { "path": "spec.mode", "value": "Create" }
]
```

Use `requestConstants` when the constant must go on both POST and PUT.

### Server-inherited fields (computed + forceNew)

Fields that the server derives from a parent resource (e.g. `region`, `vpc_id` populated from the cluster baseline) should be modeled as `computed + forceNew` with no `optional`:

```json
{ "name": "region",
  "type": "string",
  "computed": true,
  "forceNew": true,
  "apiPath": "spec.region" }
```

`UseStateForUnknown` is attached, so these fields stay quiet (no "known after apply") after the first apply.

### Avoiding perpetual drift

- **Don't read back server-normalized values** if the backend transforms what you send (e.g. lowercases a tag key). Either model the field as `optional+computed` so Terraform accepts what the server returns, or omit the `responsePath` so the user-supplied value is preserved in state.
- **Tags and system-injected fields** — if the backend injects its own entries (e.g. `duplocloud.ai/*` tags) that the user didn't set, remove the `responsePath` from those fields to avoid perpetual drift.
- **`provisioner_type` / `provisioner_version`** — the backend may return a different value than the user set (e.g. `DirectApiCall` vs `Cli`). Use `optional+computed` with no static `default` so `UseStateForUnknown` preserves the state value across applies.

---

## 8. Writing examples and generating docs

### Examples directory structure

```
examples/resources/duploai_<name>/
├── resource.tf   # one or more HCL examples
└── import.sh     # terraform import command with comments
```

`resource.tf` should contain at minimum a basic example. Add more blocks for common variations (e.g. VPC config, container image vs S3 deployment). `tfplugindocs` embeds these directly into the generated `docs/resources/<name>.md`.

`import.sh` format:

```bash
# Import an existing <resource> resource.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 6a1578ae322a8a4142bbfa04)
#  - RESOURCE_ID  is the ID of the <resource> (e.g. 6a23fee94703bc957a24eeb4)
terraform import duploai_<name>.example WORKSPACE_ID/RESOURCE_ID
```

### Regenerating docs

```bash
make doc
```

Always run this before opening a PR if you changed any spec, example, or the README. The `Generate` CI check will fail if the generated files are out of date.

---

## 9. Testing

### Unit tests

```bash
make test
# or
go test ./...
```

The engine validates every spec at load time. `TestDynamicResource` in `dynamic_resource_test.go` covers the core path-extraction, body-building, and state-mapping logic.

### Local manual testing

1. `make install` — installs the provider locally.
2. Create a test directory under `tests/<name>/` with `main.tf`, `variables.tf`, and `terraform.tfvars`.
3. Run with the dev override:

```bash
cd tests/<name>
TF_CLI_CONFIG_FILE=../.terraformrc terraform init
TF_CLI_CONFIG_FILE=../.terraformrc terraform plan
TF_CLI_CONFIG_FILE=../.terraformrc terraform apply
```

4. Test the full lifecycle: apply → verify in UI → modify a field → apply again (should update, not replace) → destroy.
5. Test import:

```bash
TF_CLI_CONFIG_FILE=../.terraformrc terraform import duploai_<name>.test WORKSPACE_ID/RESOURCE_ID
TF_CLI_CONFIG_FILE=../.terraformrc terraform plan  # should show no changes
```

### Acceptance tests

```bash
TF_ACC=1 go test ./... -v -timeout 120m
```

These run against a real backend. Not run automatically on PRs — add the `run-acceptance` label to trigger them in CI.

---

## 10. Self-reviewing before raising a PR

Before opening a PR, run a structured self-review to catch issues that will block CI or require a reviewer round-trip. The repo has a dedicated `/pr-review` skill in Claude Code that does this automatically.

### Recommended: use the `/pr-review` skill

In Claude Code, type:

```
/pr-review
```

or point it at a specific branch or PR number:

```
review the current working tree
review branch DUPLOAI-1234-my-feature
review PR 99
```

**What it checks:**

| Severity | What it looks for |
|----------|-------------------|
| 🔴 **Blocker** | Missing spec / endpoint / examples / generated docs; leaked secrets or tokens in the diff; schema errors that break build or apply (`Required+Computed`, `default` without `computed`, `Required+Optional`); sensitive fields missing `sensitive: true`; stale `make doc` output |
| 🟠 **Major** | Wrong base branch (`master` is CI/CD-only); MongoDB/ObjectId/BSON leakage in docs or examples; missing ClickUp ID in PR body; title out of 20–72 char range; PR template not filled; `forceNew` missing on immutable fields |
| 🟡 **Minor** | `gofmt` / `golangci-lint` issues; `(known after apply)` churn; wrong types; waiter misconfiguration; example HCL using real IDs |

The skill grades every finding, lists the exact file and line, and ends with one of:
- ✅ **Approve** — nothing to fix
- 🟡 **Approve with nits** — minor cosmetic issues only
- 🔴 **Request changes** — at least one Blocker or Major must be resolved

**Self-review label:** when you run `/pr-review` on your own PR the output is automatically labelled `🪞 Self-review` so reviewers know it hasn't had independent eyes yet.

### What to fix before the review is clean

The three most common Blockers caught by the skill on new resources:

1. **Missing `make doc`** — generated docs (`docs/resources/<name>.md`) or a stale README resources table. Run `make doc` and commit the output.
2. **Schema error** — an attribute has `"default"` but is missing `"computed": true`, or both `"required"` and `"computed"` are set. Fix the spec JSON.
3. **Leaked token** — a real `dahp_…` token in `terraform.tfvars` or a hardcoded hostname in an example. Move secrets to variables and ensure `*.tfvars` is gitignored.

Run the skill, fix everything graded 🔴 or 🟠, then proceed to raise the PR.

---

## 11. Raising a PR

### Recommended: use the `/pr-raise` skill in Claude Code

Once `/pr-review` comes back clean (no Blockers, no Majors), raise the PR.

The fastest and most reliable way to raise a PR is via the built-in Claude Code skill. It handles branch creation, staging, committing, validation, and the GitHub PR in one shot — and enforces all the rules in [`docs-internal/git-hygiene.md`](docs-internal/git-hygiene.md) automatically.

**How to invoke it:**

Open Claude Code in this repo and type a phrase like:

```
raise PR for DUPLOAI-1234 "Add duploai_rds_cluster resource with Multi-AZ support" against develop
```

Or shorter — the skill parses what it can from the phrase and asks for anything missing in a single question:

```
raise PR against develop
```

**What the skill does:**

1. **Validates** the PR title (20–72 chars, no ClickUp ID, correct format).
2. **Creates the branch** locally — `DUPLOAI-XXXX-<slug>` off your current HEAD — without pushing yet.
3. **Stages and commits** all unstaged changes using the PR title as the commit message.
4. **Shows a full preview** — branch, title, commits, staged files, and the complete filled-in PR body — before touching the remote.
5. **Pushes and raises** the PR on GitHub only after you approve the preview.

**What you need to have ready before invoking:**

- All changes made and tested (`make vet && make build` passing).
- `make doc` run if you changed any specs, examples, or README (CI will reject stale docs).
- Your ClickUp ticket ID (`DUPLOAI-XXXX`).
- A PR title — customer-facing, 20–72 chars, no ticket ID.

**Example session:**

```
You:    raise PR for DUPLOAI-1234 "Add duploai_rds_cluster resource with Multi-AZ support" against develop
Claude: [shows preview]
        Branch:  DUPLOAI-1234-add-duploai-rds-cluster-resource  →  develop
        Title:   Add duploai_rds_cluster resource with Multi-AZ support
        Commits: 1 commit — Add duploai_rds_cluster resource with Multi-AZ support
        Files:   duplocloud/specs/rds_cluster.json, duplosdk/rds_cluster.go, ...
        ---
        [filled PR body]
You:    Raise PR   ← approve in the UI
Claude: PR raised → https://github.com/duplocloud/terraform-provider-duploai/pull/99
```

---

### Manual flow (if not using Claude Code)

If you prefer to raise the PR manually, follow the full guide in [`docs-internal/git-hygiene.md`](docs-internal/git-hygiene.md). Quick checklist:

- [ ] Branch named `DUPLOAI-XXXX-short-description` off `develop`
- [ ] `make vet && make build` passes
- [ ] `make doc` run and generated files committed
- [ ] PR title is 20–72 characters with no ClickUp ID
- [ ] PR body uses `.github/pull_request_template.md` — ClickUp ID filled, exactly one Type checked, Overview and Summary written

### PR title examples

```
Add duploai_rds_cluster resource with Multi-AZ support
Fix waiter not retrying on transient failure state
Update cluster_attributes to support EKS managed add-ons
```

### New resource checklist

For PRs that add a new resource, the `PR Validation` CI check enforces:

- [ ] `duplocloud/specs/<name>.json` present
- [ ] `duplosdk/<name>.go` present, or inline `endpoint` in the spec
- [ ] `examples/resources/duploai_<name>/resource.tf` present
- [ ] `docs/resources/<name>.md` generated (run `make doc`)
