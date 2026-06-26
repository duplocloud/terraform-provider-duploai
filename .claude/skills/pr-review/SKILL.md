---
name: pr-review
description: >-
  Review a pull request for the terraform-provider-duploai data-driven provider.
  Use when asked to review a PR, validate a new resource, or check a branch
  before merge. Enforces no-MongoDB-in-docs, ClickUp ticket id, the repo PR
  template, the four new-resource requirements (spec / endpoint / examples /
  docs), no leaked secrets, CI-parity (vet / build / test / generate-drift /
  lint), and base-branch policy (manual PRs only on develop / hotfix/* /
  release/*; master is CI/CD-only).
---

# pr-review — terraform-provider-duploai

Review a pull request (or a branch / the working tree) for this provider against
the repo's specific conventions and CI gates. **This skill is review-only.**

## Operating contract (read first)

- **Do NOT push, merge, or edit any file.** Read and report only.
- After producing the review, ask the user: **"Do you want to post this review
  as a PR comment? (Y/N)"** Post only if the user replies `Y` or `yes` (case-
  insensitive). Never post without explicit confirmation.
- Produce one structured review in the chat, graded **Blocker / Major / Minor**,
  ending with a verdict.
- State which verification commands you actually ran and their result. Never
  claim a check passed without running it.
- If a check cannot be run (tool missing, no network), say so explicitly rather
  than guessing.

## 1. Gather the change set

Accept any of: a PR number/URL, a branch name, or "the current working tree".

Prefer `gh` when authenticated:

```bash
gh auth status                      # confirm auth first
gh pr view <pr> --json number,title,body,headRefName,baseRefName,files
gh pr diff <pr>
```

If `gh` is **not** authenticated (it has been unauthenticated in this repo
before), fall back to git:

```bash
git fetch origin
git diff --stat origin/<base>...origin/<head>
git diff origin/<base>...origin/<head>
```

For a working-tree review, use `git status` + `git diff` (staged and unstaged).

**Detect a new resource:** a newly added `duplocloud/specs/<name>.json`. When one
is present, run every Blocker check in §2. When the PR only touches engine code,
docs, or an existing resource, skip the new-resource-only blockers but run all
other checks.

**Detect a self-review:** compare the PR author against the reviewer (the person
running this skill). If they are the same person, mark the review as a
**self-review** (see §3).

```bash
# PR author (gh) or commit author (git fallback)
gh pr view <pr> --json author -q .author.login 2>/dev/null \
  || git log -1 --format='%an <%ae>' origin/<head>
# Reviewer identity
git config user.name; git config user.email
```

Match on email/login (or name as a fallback). When it matches, prepend the
self-review banner to the output and keep the same verdict rules — a self-review
is still graded honestly, just clearly labelled so a second pair of eyes knows it
hasn't had independent review.

## 2. Checklist (graded)

Severity icons (use these consistently in headings and every finding):

| Icon | Severity | Meaning |
|------|----------|---------|
| 🔴 | **Blocker** | Must fix before merge; breaks build/CI, leaks secrets, or ships a broken resource. |
| 🟠 | **Major** | Should fix before merge; policy violation (MongoDB leak, missing ClickUp id, template). |
| 🟡 | **Minor** | Nit / hygiene; safe to merge but worth addressing. |

### 🔴 Blockers — must fix before merge

For each **new resource** `<name>` (derived from the new spec's `name` field):

- [ ] **Spec JSON** `duplocloud/specs/<name>.json` exists and has the required
      top-level keys: `name`, `description`, `idPath`, `attributes`. Each
      attribute has a `name`, a `type`, and exactly one of
      `required` / `optional` / `computed` (Optional+Computed allowed).
- [ ] **Endpoint config** the spec JSON has a top-level `"endpoint"` object with a
      non-empty `"uriBase"`. Every `{placeholder}` in `uriBase` (other than `{id}`)
      must map to a string attribute in the spec. Flag any missing or empty
      `"uriBase"` — `validate()` catches it at startup, but catching it in review
      is faster. Check `"immutable": true` is set for resources with no Update path,
      and `"deprovision": {}` is present for resources the API refuses to delete
      while live.
- [ ] **`deprovisionedState` required with deprovision.** If `endpoint.deprovision`
      is present (even as `{}`), the `waiter` block **must** contain
      `"deprovisionedState"`. Without it the delete flow polls forever and times
      out after 15 minutes. This does not fail build or tests — it only surfaces at
      destroy time.

      ```bash
      # Check: deprovision present but deprovisionedState absent
      jq 'select(.endpoint.deprovision != null) | .waiter.deprovisionedState' \
        duplocloud/specs/<name>.json
      # must be a non-null string
      ```
- [ ] **Examples** `examples/resources/duploai_<name>/` contains **both**
      `resource.tf` and `import.sh`. If the spec sets `"dataSource": true`,
      `examples/data-sources/duploai_<name>/data-source.tf` must also exist —
      its absence causes `go generate` drift and fails the `Generate` CI check.
- [ ] **Generated docs** `docs/resources/<name>.md` exists and carries the
      tfplugindocs banner (`# generated by https://github.com/hashicorp/terraform-plugin-docs`).
      Docs must be generated, not hand-authored.
- [ ] **No `go generate` drift** — regenerating produces no diff (this is exactly
      what `generate.yml` fails on):

      ```bash
      go generate ./... && git status --porcelain
      ```

      Any output ⇒ docs/examples are stale ⇒ Blocker.

For **every** PR:

- [ ] **No newly introduced vulnerable dependencies (`govulncheck`).** The
      `security.yml` workflow runs `govulncheck ./...` on every PR and blocks
      merge. Run it locally when the diff touches `go.mod` / `go.sum` or adds
      any new import:

      ```bash
      govulncheck ./...   # install: go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
      ```

      Any `[vulnerability]` output is a Blocker. If `govulncheck` is not
      installed, mark this check `⚠️ skipped` and note it explicitly.

- [ ] **No secrets / sensitive data** in any tracked change. Scan the diff for:
  - bearer tokens (`dahp_…`, `Authorization: Bearer …`, `Bearer ey…`),
  - committed `*.tfvars`, `*.tfstate`/`*.tfstate.*`, `local-test/`, `main.tf`,
    `terraform-provider-*` binaries, `.mcp.json` (all are `.gitignore`d — their
    presence in the diff is a Blocker),
  - real credentials, API keys, private hostnames hardcoded as defaults.

      ```bash
      git diff origin/<base>...origin/<head> | \
        grep -nEi 'dahp_|bearer [a-z0-9._-]{12,}|password|secret|api[_-]?key|-----BEGIN'
      git diff --name-only origin/<base>...origin/<head> | \
        grep -E '\.tfvars$|\.tfstate|^local-test/|^main\.tf$|^terraform-provider-'
      ```

- [ ] **Schema correctness — build/apply-breaking (Terraform Plugin Framework).**
      These make the resource fail to build its schema or fail to plan/apply, so
      they are Blockers:
  - **No `Required` + `Computed`** on the same attribute — the framework rejects
    it. (`required` and `computed` both true in the spec ⇒ invalid.)
  - **`default` requires `computed`.** Any attribute with a `default` must also
    set `computed: true` (normally `optional` + `computed` + `default`). The
    engine wires the default unconditionally, but the framework hard-errors
    `"Default set, but Computed is false"` at schema-construction time — so an
    `optional`-only attribute with a `default` ships a resource that fails to
    build. The spec's `validate()` does **not** catch this; it only surfaces when
    the schema is instantiated (`go generate` / `go test`). Grep the spec for any
    attribute with `default` but no `computed`.
  - **No `Required` + `Optional`** on the same attribute — nonsensical and
    framework-invalid. Not rejected at spec-load, so flag it here.
  - **Server-defaulted/normalized field must be `Optional+Computed`, not
    `Optional`-only.** If the API fills in or rewrites a value the user didn't
    set (default, case-fold, canonical form), an Optional-only attribute triggers
    `"provider produced inconsistent result after apply"` on *every* apply. Make
    it Optional+Computed (and give it the right `default`).
  - **Optional+Computed with no `default` that the server does not always
    return** ⇒ the engine keeps the plan value `unknown` ⇒ inconsistent-result
    error. Either add a `default` or confirm the create/read response always
    populates it. (If it only causes `(known after apply)` churn, drop to Minor.)
  - **Secret-bearing attribute must set `sensitive: true`** (tokens, passwords,
    keys, connection strings). Missing it leaks the value into plan output and
    state — treat as a secret-exposure Blocker.

### 🟠 Major

- [ ] **Base-branch policy.** This repo's branching model:
  - `develop` — default integration branch; all regular feature/fix PRs target here.
  - `hotfix/*` — hotfix stabilisation branches; PRs may target these directly.
  - `release/*` — release stabilisation branches; PRs may target these directly.
  - `master` — **CI/CD-only.** PRs to `master` are raised exclusively by GitHub
    Actions workflows (release/hotfix promotion). A manually opened PR whose base
    is `master` is always a policy violation.

  Check the base branch and flag as Major if it is `master` or any branch other
  than `develop`, `hotfix/*`, or `release/*`:

  ```bash
  gh pr view <pr> --json baseRefName -q '.baseRefName'
  ```

- [ ] **No MongoDB / internal-database leakage** in any doc, example, comment, or
      schema description. MongoDB is the internal database and must never appear
      in public-facing text. Use neutral wording like "unique identifier".

      ```bash
      git diff origin/<base>...origin/<head> | grep -niE 'mongo|objectid|bson|\bdocument db\b'
      # Also scan the rendered docs/examples that ship to the registry:
      grep -rniE 'mongo|objectid|bson' docs/ examples/
      ```

      Flag every hit with file:line and the suggested neutral replacement.
- [ ] **ClickUp id present in body** — the PR **body** (not the title) contains a
      `DUPLOAI-\d+` reference. CI auto-strips the ticket ID from the title and
      rewrites it; the body is the required location.

      ```bash
      gh pr view <pr> --json body -q '.body' | grep -oE 'DUPLOAI-[0-9]+'
      # Also confirm the title is clean (no ticket ID remaining after auto-strip):
      gh pr view <pr> --json title -q '.title' | grep -oE 'DUPLOAI-[0-9]+'  # should be empty
      ```

- [ ] **PR title hygiene.** Title must have no ClickUp ticket ID and be 20–72
      characters. Titles become changelog entries verbatim — quality here is
      quality in the published release notes.

      ```bash
      title=$(gh pr view <pr> --json title -q '.title')
      echo "Length: ${#title}"                   # must be 20–72
      echo "$title" | grep -oE 'DUPLOAI-[0-9]+'  # must be empty
      ```

      Flag as Major if length is outside 20–72 or ticket ID is still present.

- [ ] **PR body follows the template** (`.github/pull_request_template.md`).
      Confirm these sections are present and filled, not left as placeholders:
      `ClickUp Ticket`, `Type`, `Overview`, `Summary of changes`,
      `Testing performed` (with at least one box checked),
      `Describe any breaking changes`, and the `Type` section has **exactly
      one** checkbox checked from: `enhancement`, `bug`, `breaking-change`,
      `documentation`.

      ```bash
      gh pr view <pr> --json body -q '.body' | grep -cE '- \[x\] `(enhancement|bug|breaking-change|documentation)`'
      # must equal 1; 0 = no type selected, >1 = multiple selected
      ```
- [ ] **`apiPath` missing on body fields.** Any top-level attribute that is
      **not** a `{placeholder}` in `uriBase` but has no `apiPath` is silently
      treated as a URL path parameter and never sent in the request body. No
      build error, no test failure — the field just disappears from every API
      call. For each new spec, extract the URL placeholders and compare against
      attributes that are missing `apiPath`:

      ```bash
      # URL placeholders (excluding {id})
      jq -r '.endpoint.uriBase' duplocloud/specs/<name>.json \
        | grep -oE '\{[^}]+\}' | tr -d '{}' | grep -v '^id$'
      # Attributes with no apiPath
      jq -r '.attributes[] | select(.apiPath == null) | .name' \
        duplocloud/specs/<name>.json
      ```

      Flag any attribute that appears in the second list but *not* in the first
      (i.e., not a URL placeholder) and is `required` or `optional`.

- [ ] **Schema correctness — drift / update-breaking.** These don't crash apply
      but degrade correctness, so they are Major:
  - **`forceNew` on immutable inputs.** Any field the API has no update path for
    (path params like `workspace_id`, identity fields, anything only the Create
    body accepts) must set `forceNew`. Missing it ⇒ a changed value silently
    no-ops or the update call fails. Cross-check the spec's `forceNew` flags
    against the spec's `"endpoint"` block (`"immutable": true` means no Update at
    all; `createOnly` on individual attributes means those fields are not sent on
    update).
  - **`default` must match the server's actual default.** A `default` that
    differs from what the API assigns shows perpetual drift. Verify against the
    real response (the live test in `local-test/` is the source of truth).
  - **Enum fields use `oneOf`.** String attributes with a fixed value set should
    declare `oneOf` so bad values fail at plan time, not apply time. Note `oneOf`
    is only wired for `string` attributes — on any non-string type it is silently
    ignored (no validation, no error), so an `int`/`number` enum needs a different
    guard.
  - **Required fields the server normalizes** (e.g. case-folds) show a permanent
    diff, since Create keeps the plan value while a later Read refreshes from the
    server. Flag and recommend Optional+Computed or server-side guidance.

### 🟡 Minor / standard provider hygiene

- [ ] `gofmt` clean and `golangci-lint` clean (mirrors `lint.yml`):

      ```bash
      gofmt -l .                 # any output = unformatted files
      golangci-lint run ./...    # if installed
      ```

- [ ] `go vet`, build, and unit tests pass (mirrors `ci.yml`):

      ```bash
      go vet ./... && go build -o /dev/null ./...
      # -race is required: CI (ci.yml) runs with CGO_ENABLED=1 and -race to catch
      # data races in the async waiter. A pass without -race can still fail CI.
      CGO_ENABLED=1 go test -race ./... -timeout 120s -parallel 4
      ```

- [ ] **CodeQL** runs on every PR to `develop`, `hotfix/*`, `release/*` and
      blocks merge. It cannot be run locally — mark as `⚠️ not locally verifiable`
      and note that it will run automatically in CI.

- [ ] **`(known after apply)` churn** — an Optional+Computed field whose server
      value is stable across plans but shows as unknown on every plan is a
      cosmetic nit (the apply-breaking variants are graded above). Note it.
- [ ] **Sensible types** — `int`/`number` for numeric fields (not `string`),
      `list`/`set`/`map` matching the API shape; `set` over `list` when order is
      not significant.
- [ ] **Waiter sanity** (if the spec has a `waiter`): `statusPath` and
      `successState` set; failure states cover the real terminal-failure values;
      poll interval and timeouts are reasonable.
- [ ] **Composite id consistent** — `idPath` in the spec, the `workspace_id/id`
      format in `import.sh`, and the docs all agree.
- [ ] **Example HCL uses placeholders** (`<workspace-id>`, `<scope-id>`), never
      real ids, hostnames, or tokens.
- [ ] **No dead code** introduced (golangci-lint `unused` will flag it).

## 3. Output format

Produce a single review. If this is a self-review (author == reviewer, per §1),
start with the banner line shown below; otherwise omit it.

```
> 🪞 **Self-review** — this PR was reviewed by its own author (<who>). Findings
> are graded normally, but this has not had independent review.

## PR review — <title> (#<num>)

### 🔴 Blockers
- <file:line> — <issue> — <suggested fix>
  (or: none)

### 🟠 Major
- <file:line> — <issue> — <suggested fix>
  (or: none)

### 🟡 Minor
- <file:line> — <issue> — <suggested fix>
  (or: none)

### Checks run
Prefix each result with a status icon: ✅ pass/clean · 🔴 blocker-level failure ·
🟠 major issue · ⚠️ skipped or not verifiable.

- review type: <✅ independent | 🪞 self-review (<who>)>
- base branch: <✅ develop | hotfix/… | release/… | 🟠 INVALID — <branch>>
- go generate drift: <✅ clean | 🔴 N files drifted>
- gofmt: <✅ clean | 🔴 list>
- go vet / build / test (-race): <✅ pass | 🔴 fail + output>
- govulncheck: <✅ clean | 🔴 vulnerabilities found | ⚠️ skipped — not installed>
- dependency-review: <⚠️ not locally verifiable — runs in GH Actions>
- codeql: <⚠️ not locally verifiable — runs in GH Actions>
- secret scan: <✅ clean | 🔴 hits>
- schema correctness: <✅ ok | 🔴 build/apply-breaking | 🟠 drift/update risk>
- apiPath body-field trap: <✅ clean | 🟠 N fields missing apiPath>
- mongo scan: <✅ clean | 🟠 hits>
- ClickUp id (body): <✅ DUPLOAI-NNNN | 🟠 MISSING>
- ClickUp id (title): <✅ clean | 🟠 ticket ID present in title>
- PR title length: <✅ N chars (20–72) | 🟠 too short/long — N chars>
- PR type checkbox: <✅ enhancement | bug | breaking-change | documentation | 🟠 none checked | 🟠 multiple checked>
- PR template: <✅ complete | 🟠 missing sections | ⚠️ not verifiable>

### Verdict
<✅ Approve | 🟡 Approve with nits | 🔴 Request changes>
```

**Verdict rule:** any Blocker ⇒ **Request changes**. Majors with no Blockers ⇒
Request changes unless trivial. Only Minors ⇒ Approve with nits.

### Post-review prompt

After outputting the full review, always ask:

> **Do you want to post this review as a PR comment? (Y/N)**

If the user replies `Y` or `yes`, post the review body verbatim as a PR comment:

```bash
gh pr comment <pr> --body "$(cat <<'EOF'
<full review text pasted here>
EOF
)"
```

If `gh` is unauthenticated or the PR number is unknown (working-tree review),
say so and skip posting rather than erroring silently.

## 4. Guardrails (restate)

- No comments posted unless the user explicitly confirms with `Y` / `yes`.
- Nothing pushed, no files edited, ever.
- If `gh` is unauthenticated, say so and use the `git diff` fallback for gathering diffs; skip posting even if the user confirms.
- Report command results faithfully — if a check failed or was skipped, say so.
