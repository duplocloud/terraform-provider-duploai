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
in [`docs-internal/git-hygiene.md`](../../../docs-internal/git-hygiene.md).

## Operating contract

- **Approval gate is mandatory.** Never call `gh pr create` without first showing
  the exact PR title and body to the user and receiving explicit approval.
- **Always create the working branch.** Never assume the user is already on the
  right branch. Branch creation (Step 2) always runs.
- **Collect all inputs in one round.** Gather every required value in Step 1
  before doing any git work. Never ask mid-flow for something that could have
  been asked upfront.
- Validate inputs before building the body; fail fast with a clear error message.

---

## Step 1 — Collect and validate all inputs

Extract from the invocation phrase. Ask for anything missing in a **single**
`AskUserQuestion` call — do not ask one at a time when multiple are missing.

### Required inputs

| Input | Rules |
|---|---|
| **ClickUp ID** | Must match `DUPLOAI-\d+`. Goes in the PR body, never the title. |
| **PR title** | 20–72 characters. No ClickUp ID. Written as a customer-facing changelog entry. |
| **Target branch** | Must be `develop`, `hotfix/<version>`, or `release/<version>`. Reject `master` — it is CI/CD-only. |
| **PR type** | Exactly one of: `enhancement`, `bug`, `breaking-change`, `documentation`. |
| **Overview** | 1–2 sentences on *why* this change exists. Do not invent. |
| **Summary of changes** | Bullet list of what changed. Do not invent. |
| **Draft PR?** | Yes / No. Default: No. Use Yes for WIP or when no commits exist yet. |

### Title validation

```
length = len(title.strip())
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

### Check for uncommitted changes first

```bash
git status --short
```

If there are uncommitted changes, warn the user:
> "You have uncommitted changes. They will follow to the new branch. Commit
> them there after branch creation, or stash them now if unrelated to this PR."

Do **not** block on this — `git checkout -b` carries uncommitted changes to the
new branch automatically. This is informational only.

### Derive branch name

1. **Slugify the PR title:** lowercase → spaces to `-` → strip non-alphanumeric/hyphen →
   collapse consecutive `-` → truncate at the last `-` boundary before 40 chars →
   strip leading/trailing `-`.

   Example: `"Add S3 bucket resource with versioning support"` →
   `add-s3-bucket-resource-with-versioning` (38 chars, clean word boundary)

2. **Base name:** `<DUPLOAI-ID>-<slug>`

   Example: `DUPLOAI-1641-add-s3-bucket-resource-with-versioning`

3. **Counter logic:** if the base name is taken locally or remotely, append `-01`,
   `-02`, … up to a maximum of `-03`. If all are taken, ask the user for a
   custom suffix.

```bash
branch_exists() {
  git branch --list "$1" | grep -q "$1" || \
  git ls-remote --heads origin "$1" | grep -q "$1"
}

base="DUPLOAI-<id>-<slug>"
name="$base"
counter=1
while branch_exists "$name" && [ $counter -le 3 ]; do
  name=$(printf "%s-%02d" "$base" $counter)
  counter=$((counter + 1))
done
if branch_exists "$name"; then
  # Ask user for a custom suffix — all 99 slots taken
fi
```

Counter progression example:
```
DUPLOAI-1641-add-s3-bucket-resource        ← base (free → use)
DUPLOAI-1641-add-s3-bucket-resource-01     ← if base taken
DUPLOAI-1641-add-s3-bucket-resource-02     ← if -01 taken
```

### Create and push

```bash
git checkout -b <branch-name>
git push -u origin <branch-name>
```

Tell the user: `Created and pushed branch: <branch-name>`

---

## Step 3 — Report commit status

Inform the user how many commits the PR will include (do not block on zero):

```bash
git log origin/<target>..HEAD --oneline
```

- **Commits exist** → "PR will include N commit(s): [list]"
- **No commits yet** → "Branch has no commits ahead of `<target>` yet. That is
  normal if you are raising the PR first and will add commits next. The PR is
  being created as a draft automatically." (switch `--draft` to true)

---

## Step 4 — Build PR body

Construct the body from the collected inputs using the repo's PR template structure:

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

<overview text from Step 1>

## Summary of changes

<summary bullets from Step 1>

## Testing performed

- [ ] Using unit tests
- [ ] Using acceptance tests (`TF_ACC=1`)
- [ ] Manually, on my local system
- [ ] Manually, on a remote test system

## Describe any breaking changes

<"None." if type is not breaking-change; otherwise the breaking-change description from Step 1>
```

Set `[x]` on the checkbox matching the chosen PR type; leave the others `[ ]`.

---

## Step 5 — Approval gate (mandatory)

Present the complete PR using `AskUserQuestion` with options **Raise PR** and
**Edit**, using the `preview` field to show exactly what will be sent to GitHub:

```
Branch:  <branch-name>  →  <target-branch>  [DRAFT if applicable]
Title:   <pr title>

---

<full pr body>
```

Only proceed to Step 6 when the user selects **Raise PR**.
If **Edit**: ask which field to change (title, type, overview, summary, breaking
changes), update it, re-validate if title changed, then re-show the gate.

---

## Step 6 — Raise the PR

Write the body to a temp file to avoid heredoc/shell-escaping issues, then create:

```bash
body_file=$(mktemp /tmp/pr-body-XXXXXX.md)
cat > "$body_file" << 'BODY'
<pr-body>
BODY

gh pr create \
  --base <target-branch> \
  --head <branch-name> \
  --title "<pr-title>" \
  --body-file "$body_file" \
  [--draft if applicable]

rm -f "$body_file"
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
| All counter slots taken | "All branch name variants up to `-03` are taken. Provide a custom suffix." |
| `gh` not authenticated | "GitHub CLI is not authenticated. Run `gh auth login` and retry." |
