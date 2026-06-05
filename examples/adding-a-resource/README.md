# Adding a new resource

A resource is **two files** — no engine code, no per-resource CRUD. This folder
is a worked reference built around an example resource, `foo`. The files here
are intentionally *not* compiled or registered (the Go file ends in
`.go.example`, and the JSON lives outside `duplocloud/specs/`).

| File | Goes in | Defines |
|------|---------|---------|
| `<name>.json` | `duplocloud/specs/` | the Terraform **schema** + API field mapping + waiter |
| `<name>.go`   | `duplosdk/`         | the API **URIs and verbs** |

Reference files here:

- [`foo.json`](foo.json) — a catalog with **one attribute per supported type**.
- [`foo_endpoint.go.example`](foo_endpoint.go.example) — the matching endpoint.

The two are linked by name: the spec's `"name"` must equal the
`RegisterEndpoint("<name>", …)` key. At startup the provider pairs them and
fails loudly if one is missing or a `{placeholder}` has no matching attribute.

## Steps

1. Copy `foo.json` → `duplocloud/specs/myresource.json`, trim to the types you need.
2. Copy `foo_endpoint.go.example` → `duplosdk/myresource.go`, edit the URIs.
3. `go build ./... && go test ./...`
4. The resource is now served as `duploai_myresource`. No other file changes.

## What the engine does with it

- **Schema** is built at runtime from `attributes` (required/optional/computed,
  defaults, `oneOf`, `forceNew`, sensitive — for any type).
- **stateToAPI** (`Create`/`Update`): each sendable attribute's value is placed
  into the request body at its `requestPath` (or `apiPath`); `requestConstants`
  are injected; path params go into the URL, not the body.
- **apiToState** (`Read`/import, and computed fields after create): values are
  pulled from the response at each attribute's `responsePath` (or `apiPath`).
- **Waiter**: when present, `Create`/`Update` poll the read endpoint until
  `statusPath` reaches `successState` (or a `failureStates` entry).

## Supported types (see `foo.json` for one of each)

| `type` | Notes |
|--------|-------|
| `string`, `bool`, `int`, `number` | primitives; support `default`, `oneOf` (string), `sensitive`, `forceNew` |
| `list(T)`, `set(T)`, `map(T)` | T is any primitive (`map` keys are strings) |
| `object` | single nested object; needs `attributes` |
| `list(object)`, `set(object)`, `map(object)` | collection of nested objects; needs `attributes` |

Objects nest to any depth — a nested field is itself an attribute, so an object
can contain collections and further objects. Each nested field's
`apiPath`/`requestPath`/`responsePath` is **relative to its parent**, and an
empty path defaults to the field name.

Not yet expressible in a spec (rare): tuples and collection-of-collection
(`list(list(string))`).

## Caveats

- **Server-normalized inputs cause perpetual diffs.** Create/Update keep the
  configured value in state (to avoid "inconsistent result" errors), but a later
  Read refreshes it from the API. If the backend normalizes a value you send
  (e.g. lowercases a name, reorders a list), the post-Read state won't match the
  config and Terraform will show a permanent diff. Use a normalized value in
  config, or model the field as Computed.
- **Composite id separator.** Path-parameter values and the object id must not
  contain `/` (the id is split on `/` by position).

## Field reference

### Spec (top level)
| Field | Meaning |
|-------|---------|
| `name` | resource suffix → `duploai_<name>`; must match the endpoint key |
| `description` | shown in schema/docs |
| `idPath` | dot-path to the backend object id in the create/read response |
| `attributes` | the schema attributes (below) |
| `requestConstants` | fixed `{path, value}` fields injected into every request body (envelope/discriminator values that aren't user input) |
| `requiredIf` | `{attribute, whenAttribute, whenEquals}` — conditional-required rule checked at plan time |
| `waiter` | optional async poller (below) |

### Attribute
| Field | Meaning |
|-------|---------|
| `name` | schema attribute name (snake_case) |
| `type` | see the supported-types table above |
| `required` / `optional` / `computed` | exactly one role (Optional+Computed allowed for defaulted fields) |
| `sensitive` | hides the value in plan/CLI output |
| `forceNew` | change forces replacement (RequiresReplace) |
| `default` | static default (Optional+Computed only); JSON literal of the right type |
| `oneOf` | enum constraint (string) |
| `apiPath` | default body path both directions — `spec.region`, `status.nodes[].id` |
| `requestPath` | overrides `apiPath` for the **request** only |
| `responsePath` | overrides `apiPath` for the **response** only |
| `noSend` | maps from the response but is never sent (computed-only helpers) |
| `attributes` | nested fields, when `type` is an object form |

Attributes whose **name** matches a `{placeholder}` in the endpoint `UriBase`
are path parameters: mark them `required` + `forceNew`, give them **no**
`apiPath`, and they are routed into the URL and encoded into the composite id.

### Endpoint (duplosdk file)
| Field | Meaning |
|-------|---------|
| `UriBase` | shared path prefix; `{placeholder}`s define the scope |
| `Create` / `Read` / `Update` / `Delete` | each an `Operation{Verb, Path}` |

`Operation.Verb` defaults per op (POST/GET/PUT/DELETE). `Operation.Path` is
appended to `UriBase`; default `""` for create (the collection) and `"/{id}"`
for read/update/delete. Full URL = `UriBase + Path`.

### Waiter
| Field | Meaning |
|-------|---------|
| `statusPath` | dot-path to the status string in the read response |
| `successState` | terminal success value |
| `failureStates` | map of terminal failure value → human reason |
| `failureDetailPath` | optional path to extra error context |
| `pollIntervalSeconds` | poll cadence (default 10) |
| `createTimeoutMinutes` / `updateTimeoutMinutes` / `deleteTimeoutMinutes` | default operation timeouts (overridable by a `timeouts {}` block in HCL) |

## Path syntax

- `a.b.c` — nested object field.
- `a.b[].c` — map over an array, extracting field `c` from each element (read-back only).
- `{placeholder}` in `UriBase` — a scope path parameter (a string attribute).
- `{id}` in an `Operation.Path` — the backend object id.

## Worked example (a slice of `foo`)

```hcl
resource "duploai_foo" "demo" {
  workspace_id = "ws-42"
  an_enum      = "large"
  tags         = ["a", "b"]
  labels       = { team = "core" }

  settings {
    timeout_ms = 500
    limits     = { conns = 10 }
  }

  ingress {
    host = "api.internal"
    port = 8080
  }
}
```

**Request** — `POST /v1/.../workspaces/ws-42/foos` (snake_case → the API names via
`apiPath`; `workspace_id` is in the URL, not the body):

```json
{
  "spec": {
    "size": "large",
    "tags": ["a", "b"],
    "labels": { "team": "core" },
    "settings": { "timeoutMs": 500, "limits": { "conns": 10 } },
    "ingress": [ { "host": "api.internal", "listenPort": 8080 } ]
  }
}
```

**Response → state** — computed fields read back via `responsePath`/`apiPath`:

```json
{ "id": "foo-9",
  "status": { "phase": "Ready", "nodes": [ {"id": "n1"}, {"id": "n2"} ] } }
```

yields `id = "ws-42/foo-9"`, `status = "Ready"`, `node_ids = ["n1","n2"]`.

## Import

```
terraform import duploai_foo.demo ws-42/foo-9
```
The composite id splits by the endpoint's path params (`workspace_id`) → the URI
is rebuilt and every attribute refreshed from the response.
