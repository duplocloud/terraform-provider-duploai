---
name: pr-raise
description: >-
  Raise a pull request for terraform-provider-duploai following the git hygiene
  rules in docs-internal/git-hygiene.md. Collects ClickUp ID, PR title, target
  branch, and PR type from the user; creates the working branch (with counter
  suffix if the name is taken); builds the PR body from the repo template;
  shows a full preview for approval; then creates the PR with gh. TRIGGER on
  phrases like "raise PR", "create PR", "open PR", "raise a PR for DUPLOAI-XXXX",
  "create PR against develop". Parse ClickUp ID, title, and target branch
  directly from the phrase when present — do not ask for values already given.
---

# pr-raise — terraform-provider-duploai

Create a pull request against this repo following the git hygiene rules defined
in [`docs-internal/git-hygiene.md`](../../docs-internal/git-hygiene.md).

## Operating contract

- **Approval gate is mandatory.** Never call `gh pr create` without first showing
  the exact PR title and body to the user and receiving explicit approval.
- **Always create the working branch.** Never assume the user is already on the
  right branch. Branch creation (Step 2) always runs.
- Collect missing required inputs by asking — do not assume or invent values.
- Validate inputs before building the body; fail fast with a clear error message.

---

## Step 1 — Parse and validate inputs

Extract from the invocation phrase (or ask if missing):

### Required inputs

| Input | Rules |
|---|---|
| **ClickUp ID** | Must match `DUPLOAI-\d+`. Goes in the PR body, never the title. |
| **PR title** | 20–72 characters. No ClickUp ID. Written as a customer-facing changelog entry. |
| **Target branch** | Must be `develop`, `hotfix/<version>`, or `release/<version>`. Reject `master` — it is CI/CD-only. |
| **PR type** | Exactly one of: `enhancement`, `bug`, `breaking-change`, `documentation`. |

If any required input is missing, ask for all missing values in a single
`AskUserQuestion` call (do not ask one at a time when multiple are missing).

### Title validation

```
length = len(title stripped of leading/trailing whitespace)
- length < 20  → fail: "PR title too short ({length} chars, min 20). Describe the change as a customer would read it in release notes."
- length > 72  → fail: "PR title too long ({length} chars, max 72). Shorten to keep the changelog readable."
- contains DUPLOAI-\d+  → fail: "Remove the ClickUp ticket ID from the title. It belongs in the ClickUp Ticket section of the body."
```

### Target branch validation

Confirm the target exists on the remote:

```bash
git ls-remote --heads origin <target> | grep -q <target> && echo "exists" || echo "not found"
```

Reject with a clear message if the branch is `master` or anything other than
`develop`, `hotfix/*`, `release/*`.

---

## Step 2 — Create working branch

Always create a new local branch from the current HEAD and push it.

### Derive branch name

1. **Slugify the PR title:** lowercase, replace spaces with `-`, strip anything
   not alphanumeric or `-`, collapse consecutive `-`, truncate to 40 chars,
   strip leading/trailing `-`.

   Example: `"Add S3 bucket resource with versioning"` → `add-s3-bucket-resource-with-versioning`

2. **Base name:** `<DUPLOAI-ID>-<slug>`

   Example: `DUPLOAI-1641-add-s3-bucket-resource-with-versioning`

3. **Counter logic:** if the base name is taken (locally or remotely), append
   `-01`, `-02`, … until a free name is found.

```bash
# Check existence (both local and remote)
branch_exists() {
  git branch --list "$1" | grep -q "$1" || \
  git ls-remote --heads origin "$1" | grep -q "$1"
}

base="DUPLOAI-<id>-<slug>"
name="$base"
counter=1
while branch_exists "$name"; do
  name=$(printf "%s-%02d" "$base" $counter)
  counter=$((counter + 1))
done
```

Examples of counter progression:
```
DUPLOAI-1641-add-s3-bucket-resource        (base, free → use this)
DUPLOAI-1641-add-s3-bucket-resource-01     (if base taken)
DUPLOAI-1641-add-s3-bucket-resource-02     (if -01 also taken)
```

### Create and push

```bash
git checkout -b <branch-name>
git push -u origin <branch-name>
```

Tell the user: `Created branch: <branch-name>`

If `git checkout -b` fails because there are uncommitted changes that block the
switch, surface the error and ask the user to commit or stash first.

---

## Step 3 — Check git state

After the branch is created, verify there are commits to include in the PR:

```bash
git log origin/<target>..HEAD --oneline
```

- **No commits ahead of target** → warn: "The branch has no commits yet relative
  to `<target>`. The PR will be empty. Add commits before raising the PR, or
  proceed to create a draft PR now."

Pause and ask the user to confirm before continuing if the warning fires.

---

## Step 4 — Build PR body

Construct the body by filling in the repo's PR template
(`.github/pull_request_template.md`) with the collected inputs.

Use this exact structure:

```
## ClickUp Ticket

**ClickUp Ticket ID:** <DUPLOAI-XXXX>

## Type

<!-- Check exactly one type that best describes this PR -->
- [<x or space>] `enhancement` — New feature or improvement
- [<x or space>] `bug` — Bug fix
- [<x or space>] `breaking-change` — Breaking change
- [<x or space>] `documentation` — Documentation only

## Overview

<Ask the user for 1–2 sentences on WHY this change exists if not provided. Do not invent this.>

## Summary of changes

<Ask the user for a bullet list of what changed if not provided. Do not invent this.>

## Testing performed

- [ ] Using unit tests
- [ ] Using acceptance tests (`TF_ACC=1`)
- [ ] Manually, on my local system
- [ ] Manually, on a remote test system

## Describe any breaking changes

<"None." if type is not breaking-change; otherwise ask the user to describe the break.>
```

Set `[x]` on the checkbox matching the chosen PR type; leave the others `[ ]`.

If Overview or Summary of changes were not supplied in the invocation, ask the
user for them **before** the approval gate — do not show a body with placeholder
text for approval.

---

## Step 5 — Approval gate (mandatory)

Present the complete PR to the user for review using `AskUserQuestion` with two
options — **Raise PR** and **Edit** — and use the `preview` field to render the
exact title and body the user will see on GitHub.

Format the preview as:

```
Branch:  <branch-name>  →  <target-branch>
Title:   <pr title>

---

<full pr body>
```

Only proceed to Step 6 when the user selects **Raise PR**.
If the user selects **Edit**, ask which part they want to change (title, type,
overview, summary, breaking changes), update that part, and re-show the approval
gate.

---

## Step 6 — Raise the PR

Once approved, run:

```bash
gh pr create \
  --base <target-branch> \
  --head <branch-name> \
  --title "<pr-title>" \
  --body "$(cat <<'EOF'
<pr-body>
EOF
)"
```

After creation:
1. Print the PR URL.
2. Remind the user to create the GitHub labels if they do not yet exist (only
   needed once per repo — see `docs-internal/git-hygiene.md` § Labels).

---

## Error reference

| Condition | Message |
|---|---|
| Target branch is `master` | "`master` is CI/CD-only. Use `develop`, `hotfix/<version>`, or `release/<version>`." |
| Title too short | "PR title too short (N chars, min 20). Write a descriptive title — it becomes the changelog entry." |
| Title too long | "PR title too long (N chars, max 72). Shorten it to keep the changelog readable." |
| ClickUp ID in title | "Remove the ClickUp ticket ID from the title. It belongs in the ClickUp Ticket section of the body." |
| Branch checkout fails (uncommitted changes) | "Uncommitted changes block branch creation. Run `git stash` or commit first." |
| `gh` not authenticated | "GitHub CLI is not authenticated. Run `gh auth login` and retry." |
