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
| `idRequestPath` | string | no | Dot-path at which to write the backend object id into the **update** body (normally `"id"`). For APIs that validate a full-document update against the id in the body rather than the route. See [Sending the id on update](#sending-the-id-on-update). |
| `attributes` | array | **yes** | Schema attributes. See [Attributes](#attributes). |
| `requestConstants` | array | no | Fixed key/value pairs injected into every request body. See [Request constants](#request-constants). |
| `createConstants` | array | no | Fixed pairs injected into **create** (POST) bodies only. Overrides `requestConstants` for the same path. |
| `updateConstants` | array | no | Fixed pairs injected into **update** (PUT) bodies only. Overrides `requestConstants` for the same path. |
| `requiredIf` | array | no | Conditional-required rules evaluated at plan time. See [RequiredIf rules](#requiredif-rules). |
| `conflictsWith` | array | no | Mutually-exclusive attribute groups enforced at plan time. See [ConflictsWith rules](#conflictswith-rules). |
| `invalidWhen` | array | no | Combinations the API rejects, failed at plan time instead of apply time. Covers numeric bounds, comparisons between attributes, and rules between leaves of one object. See [InvalidWhen rules](#invalidwhen-rules). |
| `resendCreatePathsOnUpdate` | bool | no | Also write each attribute's **create** path in the PUT body, with the same value. For a backend whose update body is a delta envelope over a record it rebuilds from that body. See [Delta envelopes over a rebuilt record](#delta-envelopes-over-a-rebuilt-record). |
| `dataSource` | bool | no | When `true`, also registers a read-only `data.duploai_<name>` data source derived automatically from this spec. See [Auto-generated data source](#auto-generated-data-source). |
| `dataSourceOnly` | bool | no | When `true`, registers **only** a read-only `data.duploai_<name>` data source — no managed resource is registered. Use this for purely read-only APIs (e.g. look-up endpoints with no create/update/delete). Implies data source semantics; `dataSource` need not also be set. |
| `waiter` | object | no | Async polling config. Required for resources that provision asynchronously. See [Waiter](#waiter). |
| `association` | object | no | Makes this a link between two existing objects rather than an object of its own. See [Association resources](#association-resources). |

---

## Association resources

Some endpoints link two objects that already exist rather than creating one:

```
POST   /v1/aiservicedesk/admin/data/workspaces/{workspace_id}/scopes/{scope_id}
DELETE /v1/aiservicedesk/admin/data/workspaces/{workspace_id}/scopes/{scope_id}
```

These break every assumption the normal CRUD shape makes. Both ids are in the
path, there is no request body, the response is empty (so the usual decode
rejects it as "no data"), there is no object id to append, and — critically —
there is **no GET for the link itself**. Whether the link exists can only be
learned by reading the parent and looking for the member.

Set `association` and the engine switches to that shape:

```json
{
  "name": "admin_workspace_scope_mapping",
  "endpoint": {
    "uriBase": "/v1/aiservicedesk/admin/data/workspaces/{workspace_id}/scopes/{scope_id}"
  },
  "association": {
    "readPath": "/v1/aiservicedesk/admin/data/workspaces/{workspace_id}",
    "memberPath": "scopeIds",
    "memberAttribute": "scope_id"
  },
  "attributes": [
    { "name": "workspace_id", "type": "string", "required": true, "forceNew": true },
    { "name": "scope_id",     "type": "string", "required": true, "forceNew": true }
  ]
}
```

| Field | Meaning |
|---|---|
| `readPath` | Absolute path of the object that owns the list, sharing `uriBase`'s path parameters. **Not** appended to `uriBase` — the parent normally sits above it. |
| `memberPath` | Dot-path to the list within that response, e.g. `"scopeIds"`. |
| `memberAttribute` | The attribute whose value is looked for in the list, e.g. `"scope_id"`. |

Behaviour:

- **create** — POSTs the resolved `uriBase` and ignores the empty response.
- **delete** — DELETEs the same path; no `/{id}` is appended.
- **read** — GETs `readPath` and treats the link as present only while
  `memberAttribute`'s value appears at `memberPath`. If it is gone, or the parent
  404s, the resource leaves state and the next plan recreates it. This is what
  makes detaching in the console show up as drift instead of silently persisting.
- **id** — the path parameters joined (`<workspace_id>/<scope_id>`), which is also
  the import id.
- **update** — never happens; there is nothing to change in place.

Rules enforced at startup:

- `readPath`, `memberPath` and `memberAttribute` are all required, and
  `memberAttribute` must name a real attribute.
- Every `{placeholder}` in `readPath` must be a path parameter of `uriBase`. An
  unknown one substitutes to empty, giving a URL that 404s — which reads as "link
  gone" and silently recreates the resource on every apply.
- Every attribute must be a `string`, `required`, `forceNew`, and a path
  parameter of `uriBase`. There is no body, so anything else could never be sent;
  and changing either end means a different link, not an edit.
- `endpoint.update` must not be declared.
- No `waiter` — the link is created synchronously.
- No `dataSource` / `dataSourceOnly` — a generated data source would GET the link
  path, which is exactly the endpoint that does not exist. Read the parent
  instead.

One caveat is the spec's job to document, not the engine's: if the parent
resource also manages the same list (e.g. `duploai_admin_workspace.scope_ids`),
the two will fight over it on every apply. Pick one owner per link and say so in
the resource description.

## Sending the id on update

The engine puts the object id in the URL (`PUT {uriBase}/{id}`) and never in the
body — `id` is injected into the schema, not declared as an attribute. Some APIs
need it in the body anyway.

DuploAI's admin entity endpoints are one: the update path loads the existing
record by route id but validates the *deserialized body*, and `Entity.Id`
self-generates a fresh identifier when the body omits it. A uniqueness check that
self-excludes by that id then excludes nothing, and the record collides with
itself:

```
status 400: Validation error
ShortName 'INSTALLER' is already used by workspace 'xforge-installer'.
```

— where `xforge-installer` *is* the workspace being updated. The console never
hits this because it PUTs the whole object, id included.

Set `idRequestPath` at the top level of the spec to match:

```json
{
  "name": "admin_workspace",
  "idPath": "id",
  "idRequestPath": "id",
  "endpoint": { "uriBase": "/v1/aiservicedesk/admin/data/Workspaces" }
}
```

Notes:

- **Update only.** The create body never carries an id — the backend assigns it.
- Applies to every real update: the normal `PUT`, the `updateAfterCreate`
  follow-up, and each `singleIntentUpdate` call.
- It is also written into the prior-state body used for the
  "nothing the API cares about changed" comparison, so an injected id cannot make
  the two sides differ and turn every apply into a pointless `PUT`.
- Opt-in. Leave it unset unless an API actually needs it; a stray `id` in the
  body is at best ignored and at worst rejected.

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
| `immutableOnceTrue` | bool | For a `bool`: reject a `true` → `false` change at plan time. See [One-way switches](#one-way-switches). |
| `default` | any | Static default value (JSON literal). Requires `computed: true` — the framework errors if `computed` is false and a default is set. |
| `oneOf` | array of strings | Enum constraint on a `string` attribute. Bad values fail at plan time. Only wired for `string` — on any other type it is silently ignored. |
| `apiPath` | string | Dot-path in the API body this attribute reads/writes (e.g. `"spec.region"`). See [Path mapping](#path-mapping). |
| `requestPath` | string | Override `apiPath` for write direction only. |
| `responsePath` | string | Override `apiPath` for read direction only. |
| `createPath` | string | Override for POST (create) body. Falls back to `requestPath`, then `apiPath`. |
| `updatePath` | string | Override for PUT (update) body. Falls back to `requestPath`, then `apiPath`. |
| `createOnly` | bool | Send this field in POST (create) only; never in PUT (update). Useful for fields immutable after creation that do not trigger replacement. |
| `noSend` | bool | Read from the response but never sent in requests. Use for computed-only output fields. |
| `sendFromState` | bool | The inverse of `noSend`: send a **computed-only** attribute in request bodies, carrying the value Terraform already holds in state. See [Server-assigned fields the API wants back](#server-assigned-fields-the-api-wants-back). |
| `normalizeCsvOrder` | bool | For a `string` field, sort its comma-separated tokens into a canonical (lexical) order before storing in state. Use for order-insensitive values the backend returns non-deterministically (e.g. AWS MSK bootstrap broker strings) to prevent perpetual refresh drift. |
| `stringBool` | bool | For a **`bool`** attribute, carry the value over the wire as the string `"true"`/`"false"` instead of a JSON boolean, and parse it back on read. Use when the field lives in a string-valued container the API cannot hold a real boolean in — chiefly a `Dictionary<string,string>` metadata map, where a JSON bool fails to deserialize (e.g. `delete_protection` at `metaData.delete_protection` on `resource_group`/`k8s_namespace`). Keeps HCL idiomatic (`delete_protection = false`) rather than forcing a quoted boolean. On read only an explicit `"true"` (case-insensitive) is true; any other non-null value is false, and an absent key stays null so a value the server dropped still surfaces as drift. Rejected at spec load on a non-`bool` type, or combined with `updateBoolTrueValue` (both rewrite the same value's wire form). |
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

### One-way switches

Some cloud settings can be turned on but never off. Azure Key Vault purge
protection is the canonical case: enable it and the vault must be destroyed and
recreated to get rid of it. Setting it back to `false` plans cleanly and then
fails deep in the apply with the provider's own error.

`immutableOnceTrue` moves that failure to plan time:

```json
{
  "name": "enable_purge_protection",
  "type": "bool",
  "optional": true,
  "computed": true,
  "default": false,
  "immutableOnceTrue": true
}
```

```
Error: Cannot disable enable_purge_protection

  This setting is one-way: once enabled the cloud provider does not allow
  turning it off again, so the change would fail during apply.

  Set it back to true, destroy and recreate the resource, or keep the config
  as-is and add lifecycle { ignore_changes = [enable_purge_protection] } to
  stop Terraform planning the change.
```

**Why it errors instead of suppressing the diff.** The obvious alternative —
quietly keep the old `true` — is not available. The plugin framework has no
`DiffSuppressFunc` (that was SDKv2); the nearest equivalent is a plan modifier,
and a plan modifier that returns a value differing from a *set config value*
makes Terraform reject the plan outright:

```
Provider produced invalid plan: planned value cty.True does not match
config value cty.False
```

So suppression would trade an apply-time error for a plan-time framework error,
and would leave config and reality silently diverged. A user who genuinely wants
the drift ignored can say so explicitly with `lifecycle.ignore_changes`, which is
the supported way to express "I know, leave it".

Rules enforced at startup: only valid on a `bool`, and rejected alongside
`forceNew` (which recreates on any change, so the plan-time check would never be
reached).

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

### InvalidWhen rules

`requiredIf` asks whether a field is set; `conflictsWith` asks whether two fields are
both set. Neither can express a numeric bound, a comparison between two attributes, or a
rule between two leaves of the same object — and APIs are full of those. Without a way to
state them, the spec can only document the rule in a description and let the apply fail
with a cloud error.

**Reach for the simpler tools first.** An unconditional bound is `min` / `max` on the
attribute, which the engine already wires to the framework's own validators; a fixed value
set is `oneOf`; a presence rule is `requiredIf` or `conflictsWith`. `invalidWhen` is for
what those cannot say — and note that the framework's stock comparison validators
(`int64validator.AtLeastSumOf` and friends) do not substitute here: they skip null values,
so a rule comparing an explicit value against a defaulted one never fires, which is
usually the case most worth catching.

`invalidWhen` states them. Each rule fires when **all** of its conditions hold:

```json
"invalidWhen": [
  {
    "attribute": "max_count",
    "when": [
      { "attribute": "enable_auto_scaling", "equals": "true" },
      { "attribute": "max_count", "lessThanAttribute": "min_count" }
    ],
    "message": "max_count must be >= min_count when enable_auto_scaling is true."
  },
  {
    "attribute": "upgrade_settings",
    "when": [
      { "attribute": "upgrade_settings.max_surge_type", "notEquals": "Default" },
      { "attribute": "upgrade_settings.max_surge_value", "greaterThan": 0 },
      { "attribute": "upgrade_settings.max_unavailable_type", "notEquals": "Default" },
      { "attribute": "upgrade_settings.max_unavailable_value", "greaterThan": 0 }
    ],
    "message": "Only one of surge / unavailable may be active."
  }
]
```

Conditions reuse the `requiredIf` operators — `equals`, `notEquals`, `isEmpty` — plus:

| Operator | Meaning |
|---|---|
| `greaterThan` / `lessThan` | numeric comparison against a literal (`int` / `number` attributes) |
| `lessThanAttribute` | numeric comparison against another attribute's value |

`attribute` may be a **dot-path** to a leaf inside an object (`upgrade_settings.max_surge_type`),
which is what makes a rule between two leaves of the same object expressible. A dot-path
descends through plain `object` attributes only — a leaf inside a `list(object)` or
`map(object)` needs an index or key that a dot-path cannot carry, so startup validation
rejects it rather than accepting a rule that would never fire. The rule's
own `attribute` field decides which field the error is reported against, so the diagnostic
points at what the user should change; it defaults to the first condition's attribute.

Semantics worth knowing:

- **A null value falls back to the attribute's `default`.** This is the difference between
  catching a bad config and missing it: a user who sets `min_count = 3` and leaves
  `max_count` alone has an invalid pair, because `max_count` defaults to 1 — and the
  config carries no value to compare. Only the numeric operators do this; there is no
  default to compare against when none is declared, and the condition then does not hold.
- **An unknown value stops evaluation.** A reference to another resource in the same apply
  is configured but not yet known, so the combination cannot be judged and must not be
  reported invalid — the same treatment `requiredIf` gives unknowns.
- **`message` is required**, and it is the whole point of the feature: it should name the
  attributes and state the rule, because it is all the user gets. Startup validation
  rejects a rule without one, a condition with more than one operator, a numeric operator
  on a non-numeric attribute, and any attribute or dot-path that does not resolve — so a
  typo fails at startup rather than silently never firing.
- Keep the rule **no stricter than the API**. Mirroring the backend's own check exactly is
  the goal: `azure_node_pool`'s upgrade rule tests both the type and the value on each
  side, because a non-`Default` type with a zero value is inert and the API accepts it.

### Delta envelopes over a rebuilt record

A sibling of the problem above, for an API whose update body is a **delta envelope**
(`spec.updateRequest.*`) applied against the stored record — while the record itself is
replaced by the body's create-shape fields (`spec.cluster.*`, `spec.database.*`).

Send only the envelope and the backend keeps the fields it recognises as *changed* and
resets every other one to its type default. Azure Managed Redis reset a non-default
`eviction_policy` to `NoEviction` on any unrelated update: Terraform state and Azure
still agreed, but the platform's stored record did not — and because the backend decides
whether to patch the cloud by diffing the envelope against that record, a later change
back to `NoEviction` was a no-op that reported success and never reached Azure.

Set it at the spec level:

```json
{
  "name": "azure_managed_redis",
  "resendCreatePathsOnUpdate": true
}
```

The PUT body then carries each attribute's create path **and** its update path with the
same value — the shape the platform's own console sends. Notes:

- Attributes with a single path (`apiPath` only) are written once; nothing is duplicated.
- `createOnly` attributes are still skipped on update — they opt out explicitly, and a
  field the API refuses on update must not reappear because of this flag.
- Only the update body changes; create is untouched.

### Server-assigned fields the API wants back

Computed-only attributes are outputs, so the engine never sends them (see the
`!a.Required && !a.Optional` gate in `bodyFromRaw`). That breaks against a backend
that **rebuilds its stored document from the request body**: anything the body
omits is dropped from the record.

Azure Managed Redis is such an API. `spec.scopeIds` links the instance to its
cloud provider account and is assigned by the platform, so it was modelled
computed-only — and every update silently wiped it from the stored record.
Confirmed live 2026-08-04.

`sendFromState: true` makes the engine send the value Terraform already holds:

```json
{
  "name": "scope_ids",
  "type": "list(string)",
  "computed": true,
  "sendFromState": true,
  "apiPath": "spec.scopeIds"
}
```

The alternative — marking the field `optional` purely so the body builder picks it
up — misrepresents a server-assigned value as user-settable and invites someone to
set it. `sendFromState` keeps the attribute honestly read-only.

Notes:

- Implies `UseStateForUnknown`: the value must be **known at plan time** to be
  sendable. (An earlier attempt using `stable: true` failed for exactly this
  reason.)
- On **create** there is no prior state, so the value is unknown and correctly
  omitted — which is what the API wants, since the server has not assigned it yet.
- Rejected at startup on a non-computed attribute, alongside `noSend`
  (contradictory), or on an `optional`/`required` attribute (redundant — those are
  already sent).
- This is a workaround for backend behaviour. The durable fix is for the API to
  merge updates into the stored entity instead of replacing it; until then, every
  server-assigned field in that resource's write path needs the flag.

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
| `readyPath` / `readyState` | string | — | **No default.** Optional secondary success gate: the resource is only ready once `statusPath` reaches `successState` **and** the value at `readyPath` equals `readyState`. Use when the wrapper status flips to `"Complete"` before a downstream signal is actually ready (e.g. an EC2 host whose status is `Complete` but `result.liveState` is not yet `"running"`). Must be set together. |
| `readyFailurePath` / `readyFailureStates` | string / object | — | **No default.** Optional failure gate on a second signal, independent of `statusPath`/`failureStates`. When the value at `readyFailurePath` is a key in `readyFailureStates`, the wait aborts immediately instead of polling to timeout. Use when the wrapper status reaches `successState` well before a downstream controller (e.g. Flux) reports whether it actually succeeded — e.g. a Kubernetes-style Ready condition's `reason` field. Must be set together. |
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

**Gated on a Kubernetes-style Ready condition (Flux CRDs — HelmRelease, GitRepository, OCIRepository, …):**
the wrapper `status` reaches `Complete` as soon as the CR is applied to the cluster, well before Flux
finishes reconciling it. Use `readyPath`/`readyState` to hold success until the CR's own `Ready`
condition is `True`, and `readyFailurePath`/`readyFailureStates` to fail fast on a terminal signal
instead of polling to timeout. A path segment of the form `key[filterKey=filterValue]` (e.g.
`conditions[type=Ready]`) selects the first array element matching that field — this is the only place
dot-paths support filtering, everywhere else a path is a plain series of key lookups (with `[]` to spread
over every element instead of matching one).
```json
"waiter": {
  "deprovisionedState": "DeProvisioned",
  "readyPath": "result.k8sResource.status.conditions[type=Ready].status",
  "readyState": "True",
  "readyFailurePath": "result.k8sResource.status.conditions[type=Stalled].status",
  "readyFailureStates": {
    "True": "Helm release failed to reconcile and Flux is not retrying further"
  },
  "failureDetailPath": "result.k8sResource.status.conditions[type=Ready].message"
}
```

**Why `Stalled`, not `Ready`'s `reason`:** kstatus's `Stalled` condition is Flux's
dedicated "reconciliation cannot make further progress, human intervention needed"
signal — it only becomes `True` once every configured retry (`remediation.retries`)
is exhausted. `Ready`'s `reason` (e.g. `InstallFailed`) can appear on a single
failed attempt that Flux is still going to retry automatically; gating on it
directly would abort the wait prematurely on a retry Flux was about to recover
from. Gating on `Stalled` instead is correct regardless of how many retries are
configured — including if a future spec exposes `remediation.retries` as an
attribute. `Ready`'s own `message` (rich, attempt-specific detail) is still worth
surfacing via `failureDetailPath` once `Stalled` has confirmed the failure is
final.

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
