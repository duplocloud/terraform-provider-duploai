---
name: pr-raise
description: >-
  Raise a pull request for terraform-provider-duploai following the git hygiene
  rules in docs-internal/git-hygiene.md. Collects ClickUp ID, PR title, and
  target branch; creates the working branch; stages and commits all changes;
  shows a one-click preview for approval; then pushes and creates the PR with
  gh. TRIGGER on phrases like "raise PR", "create PR", "open PR", "raise a PR
  for DUPLOAI-XXXX", "create PR against develop". Parse ClickUp ID, title, and
  target branch directly from the phrase when present — do not ask for values
  already given.
---

# pr-raise — terraform-provider-duploai

See [`docs-internal/git-hygiene.md`](../../../docs-internal/git-hygiene.md) for
the full conventions this skill enforces.

## Operating contract

- **Collect everything in one shot.** Ask for all missing inputs in a single
  `AskUserQuestion` call. Never ask mid-flow.
- **Nothing hits the remote until approved.** Branch creation, staging, and
  committing all happen locally. Only on approval does the skill push and raise
  the PR.
- **One approval gate.** Show the full preview once; user clicks **Raise PR** or
  **Cancel**. If they request a change, update and re-show once — no further loops.
- **Never invent content.** Overview and Summary are optional; omit them from the
  body if not provided.

---

## Step 1 — Collect inputs

Parse from the invocation phrase first. Ask for anything missing in **one**
`AskUserQuestion` call.

### Hard required (3)

| Input | Rules |
|---|---|
| **ClickUp ID** | `DUPLOAI-\d+`. In the body only — never the title. |
| **PR title** | 20–72 chars. No ClickUp ID. Customer-facing changelog entry. |
| **Target branch** | `develop`, `hotfix/<ver>`, or `release/<ver>`. `master` is rejected. |

### Soft required

| Input | When to ask |
|---|---|
| **PR type** | Ask with a 4-option question only if not stated in the phrase. One of: `enhancement`, `bug`, `breaking-change`, `documentation`. |

### Optional (use only if volunteered — never ask)

| Input | Behaviour if absent |
|---|---|
| **Overview** | Section omitted from body |
| **Summary of changes** | Section omitted from body |

### Validation

```
Title length < 20  → "Title too short ({n} chars, min 20)."
Title length > 72  → "Title too long ({n} chars, max 72)."
Title has DUPLOAI-\d+  → "Remove ticket ID from title — it belongs in the body."
Target is master  → "master is CI/CD-only. Target develop, hotfix/<ver>, or release/<ver>."
```

---

## Step 2 — Create branch (local only)

### Derive branch name

1. Slugify title: lowercase → spaces to `-` → strip non-alphanumeric/hyphen →
   collapse `-` → truncate at last `-` before 40 chars.
2. Base: `<DUPLOAI-ID>-<slug>`
3. If taken locally or on remote, try `-01` → `-02` → `-03`. If all taken, ask
   for a custom suffix.

```bash
branch_exists() {
  git branch --list "$1" | grep -q "$1" || git ls-remote --heads origin "$1" | grep -q "$1"
}
base="DUPLOAI-<id>-<slug>"; name="$base"; counter=1
while branch_exists "$name" && [ $counter -le 3 ]; do
  name=$(printf "%s-%02d" "$base" $counter); counter=$((counter + 1))
done
```

### Create (no push yet)

```bash
git checkout -b <branch-name>
```

`git checkout -b` carries any uncommitted changes to the new branch automatically.

---

## Step 3 — Stage and commit (local only)

Stage everything not covered by `.gitignore`:

```bash
git add -A
git status --short   # capture the list of staged files to show in the preview
```

Commit using the PR title as the message:

```bash
git commit -m "<pr-title>

ClickUp: <DUPLOAI-ID>"
```

If `git add -A` produces nothing (working tree already clean), skip the commit
step — existing commits on the branch are used as-is.

---

## Step 4 — Approval gate

Show the full PR in one `AskUserQuestion` preview, options: **Raise PR** /
**Cancel**.

```
Branch:  <branch>  →  <target>
Title:   <title>
Commits: <N commit(s) — list from git log origin/<target>..HEAD --oneline>
Files:   <staged file list from Step 3, or "no new changes staged">

---
## ClickUp Ticket
**ClickUp Ticket ID:** <id>

## Type
- [x] `<type>` — <label>
- [ ] ...

## Overview          ← omit entire section if not provided
<overview>

## Summary of changes   ← omit entire section if not provided
<summary>

## Testing performed
- [ ] Using unit tests
- [ ] Using acceptance tests (`TF_ACC=1`)
- [ ] Manually, on my local system
- [ ] Manually, on a remote test system

## Describe any breaking changes
<"None." or description>
```

If the user requests a change after seeing the preview, update that field,
re-validate title if it changed, and re-show the gate once.

---

## Step 5 — Push and raise PR

On approval:

```bash
git push -u origin <branch-name>
```

Then raise the PR using a temp file to avoid shell-escaping issues:

```bash
body_file=$(mktemp /tmp/pr-body-XXXXXX.md)
cat > "$body_file" <<'BODY'
<pr-body>
BODY

gh pr create \
  --base <target> \
  --head <branch> \
  --title "<title>" \
  --body-file "$body_file"

rm -f "$body_file"
```

Print the PR URL. Done.

---

## Error reference

| Condition | Message |
|---|---|
| Target is `master` | "`master` is CI/CD-only. Use `develop`, `hotfix/<ver>`, or `release/<ver>`." |
| Title too short/long | "Title too short/long (N chars). …" |
| ClickUp ID in title | "Remove ticket ID from title — it belongs in the body." |
| All counter slots taken | "All variants up to `-03` are taken. Provide a custom suffix." |
| `gh` not authenticated | "Run `gh auth login` and retry." |
