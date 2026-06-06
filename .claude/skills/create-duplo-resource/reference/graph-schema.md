# Knowledge graph — the shared brain

All three agents read and write **one knowledge graph per target resource**. The graph
is the only artifact carried between phases and across sessions. Raw swagger JSON and
raw backend source are **never** persisted — only the distilled graph. This is the core
token-optimization mechanism (Hard Rule 1).

Persisted as JSON inside the memory file `tfres-<name>-graph.md` (in a fenced code
block under frontmatter).

## Node — one API field
```json
{
  "id": "spec.natMode",            // unique: the API dot-path
  "tfName": "nat_mode",            // proposed snake_case TF attribute
  "type": "string",               // resolved TF type
  "required": false,
  "optional": true,
  "computed": true,
  "forceNew": false,
  "enum": ["None", "SingleAz", "MultiAz"],
  "default": "None",
  "apiPath": "spec.natMode",
  "noSend": false,
  "parent": "spec",               // for STRUCTURE nesting; null at top level
  "inheritedFrom": null,          // parent resource that populates this server-side
                                  // (e.g. "network_baseline") ⇒ model computed-only
  "enumWire": "string",           // "string" | "int" — how the enum serializes on the
                                  // wire (backend converter), even if swagger says int
  "provenance": ["swagger", "backend"],  // which layers filled/confirmed this node
  "notes": "server defaults to None when omitted"
}
```

## Edges
- **STRUCTURE** `parent → child` — object nesting (drives nested `attributes` in the spec).
- **VALIDATION** `field → constraint` — `{kind: requiredIf|range|regex|enum|computed|serverSet|immutable|inherited, detail}`.
  Filled by Agent 2 from the backend; may correct an Agent 1 node (e.g. a field swagger
  marks optional but the backend rejects when empty). `immutable` ⇒ `forceNew` (from the
  Hooks `GetImmutableSpecFields()`); `inherited` ⇒ `computed`-only (server-derived from a parent).
- **MAPPING** `field → tf` — request/response path split (`requestPath`/`responsePath`),
  `noSend`, `requestConstants`. Filled by Agent 3.
- **DEPENDS_ON** `resource → resource` — an FK field references another resource
  (`workspaceId`, `*Id`, id-lists). Carries `{implemented: bool, blocking: bool,
  specFile?}`. Drives parent-first build order and path-parameter vs body decisions.

## Graph-level fields
```json
{
  "resource": "network_baseline",
  "intent": "create" | "update",
  "endpoints": { "create": {...}, "read": {...}, "update": {...}, "delete": {...},
                 "deprovision": {...} | null,   // set when delete must deprovision first
                 "uriBase": ".../workspaces/{workspace_id}/.../networks" },
  "idPath": "id",
  "waiter": { ... } | null,        // include "deprovisionedState" when deprovision is set
  "deletableStatuses": [ ... ],    // from backend Hooks.GetDeletableStatuses() (informs the above)
  "decomposition": [ { "resource": "...", "boundary": "nodes under spec.x" } ],
  "dependencies": [ { "resource": "workspace", "implemented": true, "blocking": false } ],
  "nodes": [ ... ],
  "edges": [ ... ]
}
```

## How the layers enrich (do not re-read raw sources once distilled)
1. **Agent 1 (swagger)** creates nodes from the create-request schema + read-response
   schema, sets `type`/`required`/`enum`/nesting, records endpoints + idPath, provenance
   `["swagger"]`. Scope: only fields reachable from the create request or read response.
2. **Agent 2 (backend)** adds VALIDATION edges and corrects nodes (real requiredness,
   ranges, allowed values, server-set/computed), appends `"backend"` to provenance.
   Scope: only the validator/model for the create payload's fields.
3. **Agent 3 (provider)** adds MAPPING edges and emits the two files from the finished
   graph. Reads existing specs only to match conventions.

## Building order from the graph
- **DEPENDS_ON** unimplemented + blocking → build that parent first (recurse), depth-first.
- **STRUCTURE** with an independent CRUD endpoint in `endpoints` of its own → split into a
  child resource (decomposition); otherwise keep as a nested block.
