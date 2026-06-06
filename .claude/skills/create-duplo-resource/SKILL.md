---
name: create-duplo-resource
description: >-
  Create or update a DuploCloud Terraform resource in terraform-provider-duploai
  by producing duplocloud/specs/<name>.json + duplosdk/<name>.go. Learns the
  resource's API from live swagger, backend validation (duplo-ai-helpdesk), and
  provider conventions via a graph-based, 3-agent pipeline. TRIGGER on phrases like
  "create resource <x>", "create duplo resource <x>", "add terraform resource <x>",
  "update resource <x>", "implement duploai_<x>". Parse the resource name and
  create/update intent directly from the phrase — do not ask which resource.
---

# Create DuploCloud Terraform resource

Build (or update) one `duploai_<name>` resource. The provider is data-driven: a
resource is **two files** — `duplocloud/specs/<name>.json` (the schema) and
`duplosdk/<name>.go` (endpoint registration). A generic engine does the rest. The work
is *knowing the API truth*, gathered from three sources into one knowledge graph.

## Hard rules (always)
1. **Token-optimized.** Sub-agents return structured graph deltas, never file/code
   dumps. Raw swagger and backend source are distilled into the graph and discarded.
   Only the graph is carried between phases and sessions. Learn **only the APIs needed
   for creation** (the CRUD lifecycle of the target) — skip list/search/admin/unrelated.
2. **Deep knowledge.** Truth comes from the backend validator/model, not just swagger.
   When they disagree, the backend wins; record it as a VALIDATION edge.
3. **Memory-backed & resumable.** Persist the graph + a phase checkpoint after every
   phase so a session limit never loses work. On re-invocation, resume at the first
   incomplete phase.
4. **Always show the plan.** Print the resource implementation plan and get approval
   before writing any file (and after pre-flight, before deep learning).
5. **No internal-storage leakage in user-facing output.** Specs, descriptions, examples,
   import scripts, and generated docs must never expose backend storage internals.
   Specifically: never write "MongoDB", "ObjectId", "BSON", collection names, or similar
   persistence-layer terms. Refer to identifiers neutrally — e.g. "the ID of the
   workspace", not "the MongoDB ObjectId of the workspace". Realistic example id *values*
   are fine; the storage terminology is not.

## References (read on demand, not upfront)
- [reference/conventions.md](reference/conventions.md) — exact spec.json + sdk.go rules.
- [reference/graph-schema.md](reference/graph-schema.md) — the knowledge graph format.

## Inputs
- **Resource name + intent**: parse from the invocation phrase. `create resource foo` →
  name `foo`, intent create. Normalize to snake_case for the file/`name`.
- **Swagger access**: `{duplo-host}` + bearer token, needed for the live fetch. If not
  in the prompt, ask **once**. Never persist the token to memory or echo it.
- **Backend repo**: `~/work/dc/ai-helpdesk-dev/duplo-ai-helpdesk` (.NET/C#).
- **This repo**: `/Users/nikhil/go/terraform-provider-duploai`.

## Memory layout (per resource)
Under the session memory dir:
- `tfres-<name>-graph.md` — frontmatter + the knowledge graph JSON in a fenced block.
- `tfres-<name>-progress.md` — checkpoint: `phase0..phase4` each `pending|done`,
  build order, decomposition decision, open questions, dependency statuses.
- Add a one-line pointer in `MEMORY.md`.

---

## Step 0 — Activate & resume
1. Parse name + intent from the phrase. (No "which resource?" question.)
2. Read `tfres-<name>-progress.md` if it exists → resume at the first `pending` phase,
   rehydrating `tfres-<name>-graph.md`. Otherwise start fresh at Phase 0.
3. If host/token are needed for the next un-done swagger step and absent, ask once.

## Phase 0 — Pre-flight (dependency graph + decomposition)  [PLAN + APPROVAL]
Goal: decide *what* and *in what order* to build, before deep learning.

1. Fetch the swagger doc once: `GET {host}/swagger/v1/swagger.json` (raw OpenAPI, not
   the HTML index). Use WebFetch/curl with the bearer token. Locate the target
   resource's **create** path + request schema only.
2. From the create request schema, find **reference fields** (`*Id`, id-lists,
   `workspaceId`, `scopeIds`) and resolve each to another swagger resource. For each,
   check `duplocloud/specs/` for an existing spec → set `implemented`.
   - implemented → it's just an input attribute / path parameter.
   - not implemented AND the target cannot be created without it → **blocking parent**.
3. **Decomposition check**: a nested object that has its *own* CRUD endpoints in swagger
   → candidate child resource; nested objects only ever set inline → keep nested.
   Default: **single resource unless clearly independent**.
4. Initialize the graph (graph-level fields + DEPENDS_ON edges) and write memory.
5. **Show the plan**: dependency tree with build order (parents → child), which already
   exist, and the decomposition proposal (1 resource vs N, with each boundary). Get
   approval. For parent-first beyond one level deep, show the full chain and confirm.

Build order: depth-first over blocking parents, then the target. Run Phases 1–3 per
resource in that order.

## Phase 1 — Agent 1: Swagger (create-scoped)
Spawn a subagent (Explore/general-purpose). Prompt skeleton:
> You are extracting the API contract for the DuploCloud resource **`<name>`** to build a
> Terraform schema. Source: the swagger JSON already located at create path `<path>`
> (host `{host}`, token provided). **Scope strictly to the CRUD lifecycle**: the create
> (POST) request schema is the spine; pull read (GET) response, update (PUT), delete
> schemas only to fill computed/response fields and confirm the id path. Resolve `$ref`s
> only for fields reachable from these. Ignore list/search/bulk/admin/unrelated DTOs.
> For every field return a graph node per [graph-schema.md](reference/graph-schema.md):
> api dot-path, proposed snake_case tfName, TF type (string/bool/int/list(string)/
> object/list(object)…), required, enum (`oneOf`), nesting (STRUCTURE edges), and the
> CRUD endpoints + uriBase + idPath. Return **only** the JSON graph delta — no prose, no
> raw schema dumps.
> Also map the **delete lifecycle**: list any action sub-paths on the resource
> (`/{id}/deprovision`, `/{id}/can-deprovision`, `/{id}/soft-delete`, etc.). A resource
> with a `/deprovision` endpoint almost always **cannot be deleted while live** — record
> the deprovision path so Phase 2 can confirm the required pre-delete flow.

Merge the delta into the graph, provenance `["swagger"]`. Save graph + checkpoint.

**Verify swagger against the live API.** The deployed host may run a backend *older* than
`origin/main` (or vice-versa). Before trusting a field, sanity-check it appears in a live
`GET` of an existing instance when possible. Record any field that exists in the backend
model (Phase 2) but **not** in the deployed swagger/GET — those round-trip as `null` and
cause **perpetual drift** on that host. Flag them in the plan as "needs backend redeploy".

## Phase 2 — Agent 2: Backend validation (scoped)
First ensure the backend reflects `main` **without disrupting the user's checkout**:
`git -C ~/work/dc/ai-helpdesk-dev/duplo-ai-helpdesk fetch origin main` and read from
`origin/main` (do not `pull`/checkout). If the working tree is clean and the user wants
it, a `pull` is allowed — otherwise read via `git show origin/main:<file>` / grep.

Spawn a subagent. Prompt skeleton:
> Backend repo: `~/work/dc/ai-helpdesk-dev/duplo-ai-helpdesk` (.NET/C#). Read from
> `origin/main`. For the resource **`<name>`**, find its controller + model/DTO +
> validators (look in `*/Controllers`, `*/Models`, `*/Validators`, `*Dto`). **Scope to
> the create payload's fields only** — here is the field list from swagger: `<fields>`.
> For each field, determine the *real* validation: actual requiredness (vs swagger),
> allowed values, numeric/length ranges, regex, conditional requiredness (requiredIf),
> server-set/computed (never user-supplied), and any fixed envelope/discriminator fields
> the API injects. Also determine, explicitly:
> 1. **Enum wire-format.** For every enum field, check the C# enum + property attributes.
>    `[JsonStringEnumConverter]` / `[BsonRepresentation(BsonType.String)]` ⇒ the values
>    travel as **strings** (use the enum *names* in `oneOf`), even when swagger reports
>    them as integers (`[0,1]`). Return the string names and the default.
> 2. **Immutability ⇒ forceNew.** Find the resource's Hooks class
>    (`*/Hooks/<Name>/<Name>Hooks.cs`) and read `GetImmutableSpecFields()` and any
>    `OnPreUpdateAsync` name/owner guards — these are the authoritative `forceNew` fields.
> 3. **Delete lifecycle.** Read `GetDeletableStatuses()` and any `ValidateCanDeprovisionAsync`.
>    If the resource is only deletable from a deprovisioned/failed state (not while live),
>    the delete must **deprovision first** — record the deprovision endpoint + the terminal
>    deprovisioned status value (e.g. `DeProvisioned`).
> 4. **Server-derived-from-parent fields.** Note any field the service populates from a
>    linked/parent resource (e.g. region/vpc/subnets pulled from a Network) — these are
>    outputs, not inputs.
> Return **only** VALIDATION-edge deltas + node corrections per
> [graph-schema.md](reference/graph-schema.md) (append `"backend"` to provenance). No
> code excerpts, no prose.

Merge: VALIDATION edges, node corrections, `requiredIf`, `requestConstants`, enum
string-names, `forceNew` (from immutable-fields hook), the delete/deprovision lifecycle,
and inherited-from-parent flags. Backend overrides swagger on conflict. Save graph +
checkpoint.

## Phase 3 — Agent 3: Provider implementation  [PLAN + APPROVAL before write]
1. Spawn a subagent that reads [conventions.md](reference/conventions.md) and the
   canonical example (`specs/network_baseline.json`, `duplosdk/network_baseline.go`),
   then maps the finished graph → MAPPING edges: top-level path params (empty `apiPath`)
   vs body fields (explicit `apiPath`), `requestPath`/`responsePath` splits, `noSend`
   computed-only fields, `requestConstants`, `requiredIf`, and the waiter if the read
   response carries a status. Apply these **modeling rules** (learned the hard way):
   - **Inherited-from-parent ⇒ `computed` only.** A field derived server-side from a
     linked/parent resource (region/vpc/subnets/scope from a Network) is an output, not an
     input — never `optional`. Keep `forceNew` on it: the engine only attaches
     `UseStateForUnknown` when `computed && (optional || forceNew)`, so `computed+forceNew`
     keeps inherited values quiet (no "known after apply" churn, no spurious replacement).
   - **Pure server outputs** that change each apply (`status`, ARNs, endpoints) stay
     **plain `computed`** (no `forceNew`) so they correctly recompute.
   - **Server-populated nested fields** (a computed child of a configured object) ⇒
     `computed` + `noSend`.
   - **Deprovision-before-delete:** register `Deprovision: Operation{Verb: POST, Path:
     "/{id}/deprovision"}` in the sdk.go Endpoint and set `waiter.deprovisionedState` to
     the terminal status; the engine runs deprovision→wait→delete automatically.
   Return the proposed spec.json + sdk.go content as the graph's MAPPING result — still no
   writing.
2. **Show the implementation plan**: the full attribute table (tfName → type →
   req/opt/computed → forceNew → apiPath → validation), endpoints, idPath, waiter,
   `requiredIf`/`requestConstants`. For update intent, show the **diff** vs the existing
   spec and propose only the delta. Get approval.
3. On approval, write `duplocloud/specs/<name>.json` and `duplosdk/<name>.go`. For a
   split, write one pair per child resource (parents first).
4. Save checkpoint `phase3:done`.

## Phase 4 — Verify
`go build ./...` then `go test ./duplocloud/ -run TestDynamicResource` (the engine
validates specs at load). Report results. Fix spec errors and re-verify. Mark
`phase4:done` and update `MEMORY.md`.

---

## Resuming notes
- The graph is the single source of truth; if a phase is `done`, never re-fetch/re-read
  its source — trust the persisted nodes/edges.
- If decomposition/dependency produced multiple resources, the progress file tracks each
  resource's phase independently; resume the first incomplete (parent before child).
- Keep open questions in the progress file so a fresh session can ask the user precisely.
