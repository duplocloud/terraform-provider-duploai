# Provider conventions — `specs/<name>.json` + `duplosdk/<name>.go`

A resource in terraform-provider-duploai is **fully data-driven**. Implementing one
means producing exactly two files; a generic engine (`duplocloud/spec.go`,
`dynamic_resource.go`, `typesystem.go`) does the rest. Do **not** write per-resource
CRUD/schema Go code.

## File 1 — `duplocloud/specs/<name>.json`

Specs are embedded via `//go:embed all:specs` in `duplocloud/spec.go` and loaded at
provider start. The file name (minus `.json`) and the `name` field must match, and the
Terraform type becomes `duploai_<name>`.

### ResourceSpec (top level)
| field | meaning |
|---|---|
| `name` | resource suffix; `foo` → `duploai_foo`. Matches the file name. |
| `description` | human description. |
| `idPath` | dot-path to the object id in the API response (e.g. `id`). |
| `attributes` | `[]AttributeSpec` — the schema. |
| `requestConstants` | `[{path, value}]` — fixed envelope/discriminator fields injected into every request body (e.g. `{"path":"kind","value":"Network"}`). Use for required-but-not-user-configurable fields. |
| `requiredIf` | `[{attribute, whenAttribute, whenEquals}]` — conditional requiredness. |
| `waiter` | optional async poller (see below). |

### AttributeSpec
| field | meaning |
|---|---|
| `name` | Terraform attribute name (snake_case). |
| `description` | doc string. **Never expose backend storage internals** (no "MongoDB", "ObjectId", "BSON", collection names). Describe ids neutrally, e.g. "ID of the workspace". |
| `type` | one of `string`, `bool`, `int`, `list(string)`, `object`, `list(object)`, `set(object)`, `map(object)`. |
| `required` / `optional` / `computed` | TF schema behavior. `optional`+`computed` = server may default it. |
| `sensitive` | redact in plan/state. |
| `forceNew` | changing it forces replacement. The engine attaches `UseStateForUnknown` to a `computed` attribute when `computed && (optional || forceNew)` — so `optional+computed` and `computed+forceNew` stay quiet (keep prior value, no "known after apply" churn, no spurious replacement). A **pure** `computed` output (no `optional`, no `forceNew`) recomputes each apply — correct for volatile fields like `status`. |
| `default` | static default (only with optional+computed). |
| `oneOf` | enum constraint for a string. |
| `apiPath` | dot-path in the API body, e.g. `spec.region`. Read-back array extraction supported: `result.subnets[].subnetId`. |
| `requestPath` / `responsePath` | override `apiPath` per-direction when send/read paths differ. Each falls back to `apiPath`. |
| `noSend` | maps from response only, never sent (computed-only: `status`, ids). |
| `attributes` | nested `[]AttributeSpec` for object types; nests to any depth. |

### apiPath defaulting — the one sharp edge
- **Top-level** attribute with empty `apiPath` = **non-API** (not sent, not read). This is
  how path-parameter attributes like `workspace_id` are kept out of the body.
  → A top-level body field **must** declare `apiPath` explicitly, even `apiPath:"name"`.
- **Nested** field with empty `apiPath` defaults to its own `name`, relative to its parent.

### WaiterSpec (for async/provisioned resources)
`statusPath`, `successState`, `failureStates` (map of value→reason), optional
`failureDetailPath`, `pollIntervalSeconds` (default 10), and
`create/update/deleteTimeoutMinutes`. Set **`deprovisionedState`** (e.g. `"DeProvisioned"`)
for resources whose endpoint declares a `Deprovision` op — the delete flow waits for this
terminal status after deprovisioning, before issuing the delete call.

## File 2 — `duplosdk/<name>.go`

Tiny. One `init()` that registers the endpoint. `{id}` is the object id; any other
`{placeholder}` is a path parameter resolved at call time from the **matching top-level
attribute value** (so `{workspace_id}` requires a `workspace_id` attribute, typically
top-level with empty `apiPath`).

```go
package duplosdk

import "net/http"

func init() {
	const base = "/v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/networks"

	RegisterEndpoint("<name>", Endpoint{
		UriBase: base,
		Create:  Operation{Verb: http.MethodPost, Path: ""},        // POST   {base}
		Read:    Operation{Verb: http.MethodGet, Path: "/{id}"},    // GET    {base}/{id}
		Update:  Operation{Verb: http.MethodPut, Path: "/{id}"},    // PUT    {base}/{id}
		Delete:  Operation{Verb: http.MethodDelete, Path: "/{id}"}, // DELETE {base}/{id}
	})
}
```
Operation defaults if a field is empty: Create=POST `""`, Read=GET `/{id}`,
Update=PUT `/{id}`, Delete=DELETE `/{id}`.

**Deprovision-before-delete.** For resources the API refuses to delete while live (it
returns e.g. "must be deprovisioned first"), add a `Deprovision` operation. The engine
then runs deprovision → wait for `waiter.deprovisionedState` → delete → wait-gone, all
automatically. Leave it unset for resources whose `Delete` tears down directly.
```go
RegisterEndpoint("<name>", Endpoint{
	UriBase: base,
	// ... Create/Read/Update/Delete ...
	Deprovision: Operation{Verb: http.MethodPost, Path: "/{id}/deprovision"},
})
```

## Reference example
[`duplocloud/specs/network_baseline.json`](../../../../duplocloud/specs/network_baseline.json)
and [`duplosdk/network_baseline.go`](../../../../duplosdk/network_baseline.go) are the
canonical, complete example — read them before generating.

## Verify
`go build ./...` then `go test ./duplocloud/ -run TestDynamicResource` — the engine
validates the spec at load; a malformed spec fails the build/test, not at runtime.
