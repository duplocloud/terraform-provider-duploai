# Resource Spec Reference — terraform-provider-duploai

Every resource in this provider is driven by a single JSON file under
[`duplocloud/specs/`](../duplocloud/specs/). No Go code is required. This document
is the complete reference for writing a spec file.

---

## Overview

A spec file is a JSON object at `duplocloud/specs/<name>.json`. The provider
embeds every file in that directory at build time and registers one Terraform
resource per file. The resource type name is `duploai_<name>`.

**The minimum viable spec** (no waiter, default REST conventions):

```json
{
  "name": "plan",
  "description": "Manages a DuploAI plan.",
  "idPath": "id",
  "endpoint": {
    "uriBase": "/v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/Plans"
  },
  "attributes": [
    {
      "name": "workspace_id",
      "type": "string",
      "required": true,
      "forceNew": true,
      "description": "Workspace that owns this plan (path parameter)."
    },
    {
      "name": "name",
      "type": "string",
      "required": true,
      "apiPath": "name",
      "description": "Plan name."
    }
  ]
}
```

Saving this file, running `go generate ./...` (to regenerate docs), and running
`go build ./...` is all that is needed to add `duploai_plan` to the provider.

---

## Top-level fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | **yes** | Resource type suffix. Must match the file name (without `.json`). Yields `duploai_<name>` in Terraform. |
| `description` | string | **yes** | User-facing description shown in generated docs and the Terraform schema. |
| `idPath` | string | **yes** | Dot-path into the create/read response that contains the backend-assigned object ID (e.g. `"id"`, `"result.id"`). The engine composes the Terraform resource ID as `<scope_values>/<backend_id>`. |
| `endpoint` | object | **yes** | API endpoint configuration. See [Endpoint config](#endpoint-config). |
| `attributes` | array | **yes** | Schema attributes. See [Attributes](#attributes). |
| `requestConstants` | array | no | Fixed key/value pairs injected into every request body. See [Request constants](#request-constants). |
| `createConstants` | array | no | Fixed pairs injected into **create** (POST) bodies only. Overrides `requestConstants` for the same path. |
| `updateConstants` | array | no | Fixed pairs injected into **update** (PUT) bodies only. Overrides `requestConstants` for the same path. |
| `requiredIf` | array | no | Conditional-required rules evaluated at plan time. See [RequiredIf rules](#requiredif-rules). |
| `conflictsWith` | array | no | Mutually-exclusive attribute groups enforced at plan time. See [ConflictsWith rules](#conflictswith-rules). |
| `dataSource` | bool | no | When `true`, also registers a read-only `data.duploai_<name>` data source derived automatically from this spec. See [Auto-generated data source](#auto-generated-data-source). |
| `dataSourceOnly` | bool | no | When `true`, registers **only** a read-only `data.duploai_<name>` data source — no managed resource is registered. Use this for purely read-only APIs (e.g. look-up endpoints with no create/update/delete). Implies data source semantics; `dataSource` need not also be set. |
| `waiter` | object | no | Async polling config. Required for resources that provision asynchronously. See [Waiter](#waiter). |

---

## Endpoint config

The `"endpoint"` object tells the engine which URLs and HTTP verbs to use for
each CRUD operation. All per-resource Go files that used to live in `duplosdk/`
have been replaced by this block.

### Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `uriBase` | string | — | **Required.** URL path prefix for all operations. May contain `{placeholder}` tokens that map to string attributes by the same name (e.g. `{workspace_id}`). |
| `immutable` | bool | `false` | When `true`, the resource has no update operation — any change to a non-computed attribute forces resource replacement. |
| `create` | OperationSpec | see below | Override the HTTP verb and/or path for the Create operation. |
| `read` | OperationSpec | see below | Override for Read. |
| `update` | OperationSpec | see below | Override for Update. Ignored when `immutable` is true. |
| `delete` | OperationSpec | see below | Override for Delete. |
| `deprovision` | OperationSpec | absent | When present, adds a pre-delete teardown step **before** the Delete call. `{}` uses defaults. |

**OperationSpec fields:**

| Field | Default | Description |
|-------|---------|-------------|
| `verb` | see table below | HTTP method (e.g. `"POST"`, `"PUT"`, `"DELETE"`). |
| `path` | see table below | Path suffix appended to `uriBase`. May contain `{id}`. |
| `skipWhen` | absent | **Deprovision only.** Array of conditions (`attribute` + one of `equals`/`notEquals`/`isEmpty`) evaluated against prior state at delete time. When **all** hold (logical AND), the engine skips the pre-delete deprovision step and deletes directly. Use for modes with no cloud infrastructure to tear down, e.g. `"skipWhen": [{"attribute": "cloud", "equals": "K8S_ONLY"}]`. Ignored on create/read/update/delete. |

### Default REST conventions

When no override is given, the engine uses these defaults:

| Operation | Default verb | Default path suffix |
|-----------|-------------|---------------------|
| Create | `POST` | *(none — posts to `uriBase`)* |
| Read | `GET` | `/{id}` |
| Update | `PUT` | `/{id}` |
| Delete | `DELETE` | `/{id}` |
| Deprovision | `POST` | `/{id}/deprovision` |

The resolved URL is always `uriBase` + path suffix, with `{placeholder}` tokens
substituted from the plan state.

### Common patterns

**Standard mutable resource** (default conventions, no override needed):
```json
"endpoint": {
  "uriBase": "/v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/Plans"
}
```

**Mutable resource with a deprovision step** (pre-delete teardown, default POST /{id}/deprovision):
```json
"endpoint": {
  "uriBase": "/v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/AWSRdsInstances",
  "deprovision": {}
}
```

**Immutable resource** (no update — all changes force replacement):
```json
"endpoint": {
  "uriBase": "/v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/Namespaces",
  "immutable": true,
  "deprovision": {}
}
```

**Custom deprovision verb/path** (override the defaults):
```json
"endpoint": {
  "uriBase": "/v1/.../things",
  "deprovision": {
    "verb": "DELETE",
    "path": "/{id}/teardown"
  }
}
```

**Action-style API** (each operation has its own path, non-REST):
```json
"endpoint": {
  "uriBase": "/v1/svc/{tenant_id}",
  "create": { "verb": "POST", "path": "/CreateThing" },
  "read":   { "verb": "GET",  "path": "/GetThing/{id}" },
  "update": { "verb": "POST", "path": "/UpdateThing/{id}" },
  "delete": { "verb": "POST", "path": "/DeleteThing/{id}" }
}
```

### Path parameters

Every `{placeholder}` in `uriBase` (other than `{id}`) must have a matching
`string` attribute in the spec with the same name. The engine validates this at
startup. Path-parameter attributes should have no `apiPath` (so they are not
sent in the request body — they go into the URL only):

```json
{
  "name": "workspace_id",
  "type": "string",
  "required": true,
  "forceNew": true,
  "description": "Workspace ID (path parameter)."
}
```

The reserved name `id` must not appear in `attributes` — the engine injects it
automatically.

---

## Attributes

The `"attributes"` array describes the Terraform schema and how each field maps
to the API's JSON body. Each entry is an `AttributeSpec` object.

### Attribute fields reference

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | **Required.** Terraform attribute name (snake_case). Must be unique within the spec. `"id"` is reserved. |
| `description` | string | User-facing description in generated docs and the schema. |
| `type` | string | **Required.** Terraform type. See [Supported types](#supported-types). |
| `required` | bool | Attribute must be set in config. Mutually exclusive with `computed`-only. |
| `optional` | bool | Attribute may be set in config. |
| `computed` | bool | Attribute may be set by the provider (server-returned). |
| `sensitive` | bool | Value is masked in plan output and state. Use for passwords, tokens, keys. |
| `forceNew` | bool | Changing this attribute destroys and recreates the resource (`RequiresReplace`). |
| `default` | any | Static default value (JSON literal). Requires `computed: true` — the framework errors if `computed` is false and a default is set. |
| `oneOf` | array of strings | Enum constraint on a `string` attribute. Bad values fail at plan time. Only wired for `string` — on any other type it is silently ignored. |
| `apiPath` | string | Dot-path in the API body this attribute reads/writes (e.g. `"spec.region"`). See [Path mapping](#path-mapping). |
| `requestPath` | string | Override `apiPath` for write direction only. |
| `responsePath` | string | Override `apiPath` for read direction only. |
| `createPath` | string | Override for POST (create) body. Falls back to `requestPath`, then `apiPath`. |
| `updatePath` | string | Override for PUT (update) body. Falls back to `requestPath`, then `apiPath`. |
| `createOnly` | bool | Send this field in POST (create) only; never in PUT (update). Useful for fields immutable after creation that do not trigger replacement. |
| `noSend` | bool | Read from the response but never sent in requests. Use for computed-only output fields. |
| `normalizeCsvOrder` | bool | For a `string` field, sort its comma-separated tokens into a canonical (lexical) order before storing in state. Use for order-insensitive values the backend returns non-deterministically (e.g. AWS MSK bootstrap broker strings) to prevent perpetual refresh drift. |
| `preserveOnEmptyResponse` | bool | Keep the value already held for this attribute — the configured plan value on create/update, the prior state value on refresh — whenever the API returns null or empty for it. See [Write-only fields](#write-only-fields). |
| `deprecated` | string | Marks the attribute deprecated: the message is wired to the framework's `DeprecationMessage` and shown as a warning whenever the attribute is set in config. Use when renaming an attribute — keep the old one with a deprecation message pointing at the replacement (pair with `conflictsWith` + `requiredIf`/`isEmpty` for a backwards-compatible rename). |
| `attributes` | array | Nested `AttributeSpec` entries. Required when `type` is an object form. Recurses to any depth. |

### Supported types

| Type string | Terraform type | Notes |
|-------------|---------------|-------|
| `"string"` | `types.String` | Supports `default`, `oneOf`, `sensitive`, `forceNew`. |
| `"bool"` | `types.Bool` | Supports `default`, `forceNew`. |
| `"int"` | `types.Int64` | Supports `default`, `forceNew`. |
| `"number"` | `types.Float64` | Supports `default`, `forceNew`. Use for floating-point values. |
| `"list(string)"` | `types.List` of string | Ordered, allows duplicates. |
| `"list(bool)"` | `types.List` of bool | |
| `"list(int)"` | `types.List` of int64 | |
| `"list(number)"` | `types.List` of float64 | |
| `"list(object)"` | `ListNestedAttribute` | Requires `attributes`. |
| `"set(string)"` | `types.Set` of string | Unordered, deduplicated. Prefer over `list` when order doesn't matter. |
| `"set(bool)"` | `types.Set` of bool | |
| `"set(int)"` | `types.Set` of int64 | |
| `"set(number)"` | `types.Set` of float64 | |
| `"set(object)"` | `SetNestedAttribute` | Requires `attributes`. |
| `"map(string)"` | `types.Map` of string | String-keyed map. |
| `"map(bool)"` | `types.Map` of bool | |
| `"map(int)"` | `types.Map` of int64 | |
| `"map(number)"` | `types.Map` of float64 | |
| `"map(object)"` | `MapNestedAttribute` | String keys, object values. Requires `attributes`. |
| `"object"` | `SingleNestedAttribute` | Single nested object. Requires `attributes`. |

### Terraform schema combinations

| Combination | Meaning | Notes |
|-------------|---------|-------|
| `required: true` | User must supply a value. | Cannot combine with `optional` or be `computed`-only. |
| `optional: true` | User may supply a value. | Omitting leaves it null. |
| `computed: true` | Provider may set the value. | Alone = read-only output field. |
| `optional + computed` | User may set it; if not, the server fills it in. | The most common pattern for server-defaulted fields. **Must have `default`** if the server always returns a value, to avoid perpetual drift. |
| `required: false, optional: false, computed: true` | Purely computed (e.g. `status`, `id`). Not user-configurable. | |

**Rules enforced at startup:**
- Exactly one of `required` / `optional` / `computed` must be true (or `optional + computed`).
- `required + computed` is invalid (framework rejects it).
- `required + optional` is invalid.
- `default` requires `computed: true` — the framework hard-errors otherwise.

### Write-only fields

Some backends accept a value but never return it. The AI Helpdesk redacts every
sensitive credential value to `""` on the way out — on `GET`, and on the `POST`
/ `PUT` response too. Storing that empty value fails the apply with
*"provider produced inconsistent result after apply: … inconsistent values for
sensitive attribute"*, and, once past create, shows perpetual drift.

Mark such a leaf `preserveOnEmptyResponse: true`:

```json
{
  "name": "value",
  "type": "string",
  "optional": true,
  "sensitive": true,
  "apiPath": "value",
  "preserveOnEmptyResponse": true
}
```

The engine then keeps what it already had — the configured plan value on
create/update, the prior state value on refresh — whenever the response is null
or an empty string. A **non-empty** response value always wins, so a rotation
the API does surface is still picked up.

Scope and limits:

- Valid on a leaf (`string` / `bool` / `number`), top-level or nested inside an
  `object`, `list(object)` or `map(object)`. Inside a collection the prior value
  is paired positionally — list by index, map by key.
- Not applied inside `set(object)`: element order is not stable, so a prior
  element cannot be matched to a response element.
- Terraform cannot detect an out-of-band change to a field the API won't return.
  Say so in the attribute's `description`.
- On **import** there is no prior value, so the field lands empty — expected,
  since the secret is unrecoverable from the API.

### Path mapping

`apiPath` is a dot-separated path into the JSON body. The engine uses it for
both reading the response and writing the request.

**Examples:**

| `apiPath` | JSON body location |
|-----------|-------------------|
| `"name"` | `{ "name": "..." }` |
| `"spec.region"` | `{ "spec": { "region": "..." } }` |
| `"spec.provisioner.type"` | `{ "spec": { "provisioner": { "type": "..." } } }` |

**Array element extraction** (read-only, using `[]`):
```json
{
  "name": "node_ids",
  "type": "list(string)",
  "computed": true,
  "apiPath": "status.nodes[].id"
}
```
This reads `status.nodes` as an array and extracts the `id` field from each
element into a `list(string)`.

**Top-level attribute with no `apiPath`** is a path parameter — it goes into
the URL and is never sent in the body, and is never read from the response.
This is how `workspace_id` works.

**Nested attribute with no `apiPath`** defaults to the field's own `name` as
the key in the parent object. No `apiPath` needed for straightforward mappings.

**Split request/response paths** — use when the API sends and receives a value
at different locations:
```json
{
  "name": "region",
  "type": "string",
  "optional": true,
  "requestPath": "spec.region",
  "responsePath": "configuration.region"
}
```

**Per-verb path overrides** — use when create and update DTOs differ:
```json
{
  "name": "config",
  "type": "string",
  "optional": true,
  "createPath": "spec.createRequest.config",
  "updatePath": "spec.updateRequest.config"
}
```

### Attribute examples

**Required path parameter (no body mapping):**
```json
{
  "name": "workspace_id",
  "type": "string",
  "required": true,
  "forceNew": true,
  "description": "Workspace ID (URL path parameter)."
}
```

**Required body field:**
```json
{
  "name": "name",
  "type": "string",
  "required": true,
  "apiPath": "name",
  "description": "Resource name."
}
```

**Optional + computed with default (server-defaulted):**
```json
{
  "name": "description",
  "type": "string",
  "optional": true,
  "computed": true,
  "default": "",
  "apiPath": "description",
  "description": "Optional description."
}
```

**Enum string:**
```json
{
  "name": "mode",
  "type": "string",
  "optional": true,
  "computed": true,
  "default": "Create",
  "oneOf": ["Create", "Import"],
  "forceNew": true,
  "apiPath": "spec.mode",
  "description": "Provisioning mode."
}
```

**Sensitive (credentials, tokens):**
```json
{
  "name": "master_user_password",
  "type": "string",
  "optional": true,
  "sensitive": true,
  "apiPath": "spec.masterUserPassword",
  "description": "Master user password."
}
```

**Computed-only output (read from response, never sent):**
```json
{
  "name": "status",
  "type": "string",
  "computed": true,
  "apiPath": "status",
  "description": "Current provisioning status."
}
```

**Computed-only, explicitly not sent (noSend):**
```json
{
  "name": "cf_stack_name",
  "type": "string",
  "computed": true,
  "noSend": true,
  "apiPath": "result.cfStackName",
  "description": "CloudFormation stack name."
}
```

**Create-only field (immutable after creation, no replacement):**
```json
{
  "name": "code_source",
  "type": "string",
  "optional": true,
  "createOnly": true,
  "apiPath": "spec.codeSource",
  "description": "Code source URI. Set on create only."
}
```

**Nested object:**
```json
{
  "name": "components",
  "type": "object",
  "optional": true,
  "computed": true,
  "apiPath": "spec.components",
  "description": "Cluster add-ons.",
  "attributes": [
    {
      "name": "cluster_autoscaler",
      "type": "bool",
      "optional": true,
      "computed": true,
      "default": false,
      "apiPath": "clusterAutoscaler",
      "description": "Install the Cluster Autoscaler."
    }
  ]
}
```

**List of objects:**
```json
{
  "name": "parameters",
  "type": "list(object)",
  "optional": true,
  "apiPath": "spec.parameters",
  "description": "DB parameter overrides.",
  "attributes": [
    { "name": "name",  "type": "string", "required": true, "apiPath": "name" },
    { "name": "value", "type": "string", "required": true, "apiPath": "value" }
  ]
}
```

---

## Request constants

`requestConstants`, `createConstants`, and `updateConstants` inject fixed
key/value pairs into request bodies. They are useful for envelope fields or
discriminators the API requires that are not user-configurable (and therefore
should not appear in the schema).

Each entry:

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Dot-path where the value is injected in the request body. |
| `value` | any | JSON value. Can be a string, number, bool, array, or object. |

`createConstants` and `updateConstants` take precedence over `requestConstants`
when both set the same path.

**Example** — always inject `spec.mode = "Create"` into every request:
```json
"requestConstants": [
  { "path": "spec.mode", "value": "Create" }
]
```

**Example** — different values for create vs update:
```json
"createConstants": [
  { "path": "operation", "value": "create" }
],
"updateConstants": [
  { "path": "operation", "value": "update" }
]
```

---

## RequiredIf rules

`requiredIf` enforces plan-time conditional requirements: attribute `A` must be
set whenever a condition (or set of conditions) holds.

Two forms are supported:

### Simple form (single equality check)

| Field | Type | Description |
|-------|------|-------------|
| `attribute` | string | The attribute that becomes required. Must exist in `attributes`. |
| `whenAttribute` | string | The trigger attribute to watch. Must exist in `attributes`. |
| `whenEquals` | string | The value of `whenAttribute` that activates the requirement. |

**Example** — `kms_key_id` is required when `storage_encrypted` is `"true"`:
```json
"requiredIf": [
  {
    "attribute": "kms_key_id",
    "whenAttribute": "storage_encrypted",
    "whenEquals": "true"
  }
]
```

### Compound form (logical AND of multiple conditions)

Use the `when` array when the requirement depends on more than one condition.
All conditions must hold (logical AND). Each condition targets one attribute and
uses exactly one of `equals`, `notEquals`, or `isEmpty`.

| Field | Type | Description |
|-------|------|-------------|
| `attribute` | string | The attribute that becomes required. Must exist in `attributes`. |
| `when` | array | List of conditions, all of which must hold (AND). |

Each condition object:

| Field | Type | Description |
|-------|------|-------------|
| `attribute` | string | The attribute whose value is checked. |
| `equals` | string | Condition holds when the attribute equals this value. |
| `notEquals` | string | Condition holds when the attribute does **not** equal this value. |
| `isEmpty` | bool | Condition holds when the attribute is unset or empty. |

Exactly one of `equals`, `notEquals`, or `isEmpty` must be set per condition.

When evaluating, if the user omitted an attribute that has a `default`, the
default value is used — so conditions on defaulted fields work correctly
without the user explicitly setting them.

**Example** — `num_cache_clusters` is required when `engine != "Memcached"` AND `cluster_mode == "Disabled"`:
```json
"requiredIf": [
  {
    "attribute": "num_cache_clusters",
    "when": [
      { "attribute": "engine",       "notEquals": "Memcached" },
      { "attribute": "cluster_mode", "equals": "Disabled"    }
    ]
  }
]
```

**Example** — `engine_version` is required when `snapshot_name` is not set:
```json
"requiredIf": [
  {
    "attribute": "engine_version",
    "when": [
      { "attribute": "snapshot_name", "isEmpty": true }
    ]
  }
]
```

---

## ConflictsWith rules

`conflictsWith` enforces mutually-exclusive attribute groups at plan time: within
each group at most one attribute may be set.

Each entry in the array is a list of attribute names. If two or more attributes
from the same group are set, validation fails with an error on each conflicting
attribute.

**Example** — `snapshot_name` and `snapshot_arns` cannot both be set:
```json
"conflictsWith": [
  ["snapshot_name", "snapshot_arns"]
]
```

Multiple groups are independent:
```json
"conflictsWith": [
  ["snapshot_name", "snapshot_arns"],
  ["replica_of", "num_cache_clusters"]
]
```

---

## Auto-generated data source

Setting `"dataSource": true` registers a read-only `data.duploai_<name>` data
source from the same spec. No extra file is needed. The schema is derived
automatically:

- **Path-parameter attributes** (those with no `apiPath`) → `Required`
- **`id`** → `Required` (user supplies the object ID for lookup)
- **All other readable attributes** → `Computed`
- **Write-only attributes** (no `apiPath` and no `responsePath`) → excluded

The data source performs a single `GET /{id}` and populates all computed
attributes from the response.

**Example** — add a data source alongside the managed resource:
```json
{
  "name": "plan",
  "dataSource": true,
  ...
}
```

This registers both `resource "duploai_plan"` and `data "duploai_plan"`.

The data source example file must be placed at
`examples/data-sources/duploai_<name>/data-source.tf`.

---

## Waiter

The `"waiter"` key is an **explicit opt-in for asynchronous provisioning**:

| Spec | Engine behaviour |
|------|-----------------|
| `"waiter"` key **absent** | **Synchronous** — `apply` returns immediately after the API call completes. Use only when the API itself blocks until the resource is ready. |
| `"waiter": {}` or `"waiter": { ... }` | **Asynchronous** — the engine polls the resource's `status` field until it reaches a terminal state (success or failure). |

Do not omit the `"waiter"` key for a resource whose API returns immediately but whose backend provisions asynchronously — doing so makes `terraform apply` return before the resource is ready, breaking any dependent resources.

### Defaults

The engine applies the following defaults to every `"waiter"` block at load
time. A spec only needs to declare the fields that differ from these values —
most specs reduce to 1–4 fields.

| Field | Default value |
|-------|--------------|
| `statusPath` | `"status"` |
| `successState` | `"Complete"` |
| `failureDetailPath` | `"blockedReason"` |
| `failureStates` | `Failed`, `Blocked`, `WaitingForApproval`, `DeprovisionFailed` (see below) |
| `pollIntervalSeconds` | `10` |
| `createTimeoutMinutes` | `30` |
| `updateTimeoutMinutes` | `30` |
| `deleteTimeoutMinutes` | `15` |

Default `failureStates` map (identical across all resources):
```json
{
  "Failed":             "provisioning failed",
  "Blocked":            "provisioning is blocked",
  "WaitingForApproval": "provisioning is waiting for manual approval, which Terraform cannot provide",
  "DeprovisionFailed":  "deprovisioning failed"
}
```

### Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `statusPath` | string | `"status"` | Dot-path in the read response to the status string. |
| `successState` | string | `"Complete"` | The terminal success value. |
| `failureStates` | object | see above | Map of terminal failure values → human-readable error message. Overrides the default map entirely when specified. |
| `deprovisionedState` | string | — | **No default.** Terminal status after the deprovision step completes (e.g. `"DeProvisioned"`). Required when `endpoint.deprovision` is set — the delete flow waits for this state before issuing the final delete call. |
| `failureDetailPath` | string | `"blockedReason"` | Dot-path to a field with extra error context. Appended to the error message when the status is a failure state. |
| `failureRetries` | int | `0` | **No default.** Extra polls to tolerate after first seeing a failure state before treating it as terminal. Use for backends that report a transient failure and then self-recover. |
| `pollIntervalSeconds` | int | `10` | Seconds between status polls. |
| `failurePollIntervalSeconds` | int | *(same as `pollIntervalSeconds`)* | Override the poll interval when the resource is in a failure state during a retry. Use a longer value to give the backend more time to self-recover between checks. Falls back to `pollIntervalSeconds` when unset. |
| `createTimeoutMinutes` | int | `30` | Default create timeout. Overridable per-instance via the `timeouts` block. |
| `updateTimeoutMinutes` | int | `30` | Default update timeout. |
| `deleteTimeoutMinutes` | int | `15` | Default delete timeout. |

When a waiter is present, the provider automatically adds a `failure_retries`
attribute (optional `int64`) to the schema so users can override
`failureRetries` per resource instance without changing the spec.

When a waiter is present and the resource is mutable (not `immutable: true`),
a `timeouts` block is automatically added to the schema.

### Waiter patterns

**Asynchronous resource, all standard defaults (no custom timeouts or poll interval needed):**
```json
"waiter": {}
```

**With deprovision step (most resources):**
```json
"waiter": {
  "deprovisionedState": "DeProvisioned"
}
```

**Fast resource (k8s) — shorter timeouts:**
```json
"waiter": {
  "deprovisionedState": "DeProvisioned",
  "createTimeoutMinutes": 15,
  "updateTimeoutMinutes": 15,
  "deleteTimeoutMinutes": 10
}
```

**Slow resource (RDS) — longer poll and timeouts:**
```json
"waiter": {
  "deprovisionedState": "DeProvisioned",
  "pollIntervalSeconds": 15,
  "createTimeoutMinutes": 60,
  "updateTimeoutMinutes": 60,
  "deleteTimeoutMinutes": 30
}
```

**With transient-failure tolerance:**
```json
"waiter": {
  "deprovisionedState": "DeProvisioned",
  "failureRetries": 3
}
```

---

## Complete example

The following spec shows every section in use. It is based on
[`examples/adding-a-resource/foo.json`](../examples/adding-a-resource/foo.json),
the canonical reference file:

```json
{
  "name": "my_resource",
  "description": "Manages a My Resource within a workspace.",
  "idPath": "id",

  "endpoint": {
    "uriBase": "/v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/MyResources",
    "deprovision": {}
  },

  "attributes": [
    {
      "name": "workspace_id",
      "type": "string",
      "required": true,
      "forceNew": true,
      "description": "Workspace ID (URL path parameter, not sent in body)."
    },
    {
      "name": "name",
      "type": "string",
      "required": true,
      "forceNew": true,
      "apiPath": "name",
      "description": "Resource name. Immutable after creation."
    },
    {
      "name": "mode",
      "type": "string",
      "optional": true,
      "computed": true,
      "default": "Create",
      "oneOf": ["Create", "Import"],
      "forceNew": true,
      "apiPath": "spec.mode",
      "description": "Provisioning mode."
    },
    {
      "name": "instance_class",
      "type": "string",
      "required": true,
      "apiPath": "spec.instanceClass",
      "description": "Instance class (e.g. t3.medium)."
    },
    {
      "name": "token",
      "type": "string",
      "optional": true,
      "sensitive": true,
      "apiPath": "spec.token",
      "description": "Auth token (masked in plan output)."
    },
    {
      "name": "tags",
      "type": "map(string)",
      "optional": true,
      "apiPath": "spec.tags",
      "description": "Resource tags."
    },
    {
      "name": "settings",
      "type": "object",
      "optional": true,
      "computed": true,
      "apiPath": "spec.settings",
      "description": "Additional settings.",
      "attributes": [
        {
          "name": "timeout_ms",
          "type": "int",
          "optional": true,
          "apiPath": "timeoutMs",
          "description": "Request timeout in milliseconds."
        }
      ]
    },
    {
      "name": "status",
      "type": "string",
      "computed": true,
      "apiPath": "status",
      "description": "Current provisioning status."
    }
  ],

  "requestConstants": [
    { "path": "kind", "value": "MyResource" }
  ],

  "requiredIf": [
    {
      "attribute": "token",
      "whenAttribute": "mode",
      "whenEquals": "Import"
    }
  ],

  "waiter": {
    "deprovisionedState": "DeProvisioned"
  }
}
```

---

## Quick checklist when adding a new resource

1. **Create `duplocloud/specs/<name>.json`** with `name`, `description`,
   `idPath`, `endpoint.uriBase`, and `attributes`.
2. **Add `workspace_id`** (or whatever path parameter(s) appear in `uriBase`)
   as `required + forceNew` with no `apiPath`.
3. **Set `forceNew: true`** on every field the API cannot update in place.
4. **Set `immutable: true`** in `endpoint` if the API has no PUT operation at all.
5. **Add `"deprovision": {}`** in `endpoint` if the API requires a teardown step
   before deletion, and add `deprovisionedState` to the `waiter`.
6. **Add a `waiter`** if provisioning is asynchronous (most resources here are). Minimum is `"waiter": {}` — defaults cover `statusPath`, `successState`, `failureStates`, `failureDetailPath`, and standard timeouts. Only set fields that differ (e.g. `deprovisionedState`, non-standard timeouts, `failureRetries`).
7. **Run `go generate ./...`** to regenerate docs, then
   **run `go build ./...` and `go test ./...`** to verify the spec is valid.
8. **Add `examples/resources/duploai_<name>/resource.tf`** and
   **`import.sh`** — required by CI.
9. **If `dataSource: true`**, add
   **`examples/data-sources/duploai_<name>/data-source.tf`** — also required by CI.

## Common mistakes

| Mistake | Symptom | Fix |
|---------|---------|-----|
| `default` without `computed: true` | Provider panics at startup: `"Default set, but Computed is false"` | Add `"computed": true`. |
| `required: true` on a top-level body field but no `apiPath` | Field silently goes into the URL, not the body | Add `"apiPath": "<field>"`. |
| `deprovision: {}` in endpoint but no `deprovisionedState` in waiter | Delete hangs until timeout — waiter never sees the pre-delete terminal state | Add `"deprovisionedState": "DeProvisioned"` (or whatever the API returns). |
| Path parameter not in `attributes` | Provider panics at startup: `endpoint path references unknown attribute {x}` | Add the attribute as a `string + required + forceNew` with no `apiPath`. |
| `required + computed` on the same attribute | Provider panics at schema build: framework rejects it | Use `optional + computed` instead. |
| Mutable field missing `forceNew` | Terraform silently no-ops or the update call fails (field not in PUT body) | Add `"forceNew": true` or `"createOnly": true` depending on semantics. |
| `oneOf` on a non-string type | No validation, no error — constraint is silently ignored | Only `oneOf` on `"type": "string"` attributes; use a different guard for other types. |
| `requiredIf` compound rule missing `when` | Rule is silently skipped — `whenAttribute`/`whenEquals` are required for the simple form | Use `"when": [...]` for multi-condition rules; use `"whenAttribute"` + `"whenEquals"` for single-condition rules. |
| `dataSource: true` but no `examples/data-sources/` file | CI fails | Add `examples/data-sources/duploai_<name>/data-source.tf`. |
