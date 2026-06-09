---
name: pr-raise
description: >-
  Raise a pull request for terraform-provider-duploai following the git hygiene
  rules in docs-internal/git-hygiene.md. Collects ClickUp ID, PR title, target
  branch, and PR type from the user; builds the PR body from the repo template;
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
- Collect missing required inputs by asking — do not assume or invent values.
- Validate inputs before building the body; fail fast with a clear error message.
- This skill does **not** commit or push code. If uncommitted changes or an
  unpushed branch are detected, warn the user and pause until resolved.

---

## Step 1 — Parse and validate inputs

Extract from the invocation phrase (or ask if missing):

### Required inputs

| Input | Rules |
|---|---|
| **ClickUp ID** | Must match `DUPLOAI-\d+`. Goes in the PR body, never the title. |
| **PR title** | 20–72 characters. No ClickUp ID. Written as a customer-facing changelog entry. |
| **Target branch** | Must be `develop`, `hotfix/<version>`, or `release/<version>`. Reject `master` or any other branch — it is CI/CD-only. |
| **PR type** | Exactly one of: `enhancement`, `bug`, `breaking-change`, `documentation`. |

If any required input is missing, ask for all missing values in a single
`AskUserQuestion` call (do not ask one at a time when multiple are missing).

### Title validation

Before proceeding, check the title:

```
length = len(title stripped of leading/trailing whitespace)
- length < 20  → fail: "PR title too short ({length} chars, min 20). Describe the change as a customer would read it in release notes."
- length > 72  → fail: "PR title too long ({length} chars, max 72). Shorten to keep the changelog readable."
- contains DUPLOAI-\d+  → fail: "Remove the ClickUp ticket ID from the title. It belongs in the ClickUp Ticket section of the body."
```

### Target branch validation

```bash
# Confirm the branch exists on the remote (or at least locally)
git branch --list "<target>"
gh api repos/{owner}/{repo}/branches/<target> 2>/dev/null || echo "not found on remote"
```

Reject with a clear message if the branch is `master` or anything other than
`develop`, `hotfix/*`, `release/*`.

---

## Step 2 — Check git state

Run the following and surface any issues to the user before building the body:

```bash
git status --short          # uncommitted changes?
git log origin/<target>..HEAD --oneline  # commits not yet on remote?
git rev-parse --abbrev-ref HEAD          # current branch name
```

- **Uncommitted changes** → warn: "You have uncommitted changes. Commit or stash them before raising the PR, or proceed knowing only pushed commits will be included."
- **Nothing pushed (no commits ahead of remote)** → warn: "No commits are pushed to the remote for this branch yet. The PR will be empty. Push first with `git push -u origin <branch>`."
- **Branch not pushed at all** → error: "Branch `<branch>` has no upstream. Run `git push -u origin <branch>` first."

Pause and ask the user to confirm they want to continue if any warning fires.

---

## Step 3 — Build PR body

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

<Ask the user for 1–2 sentences on WHY this change exists if not provided in the invocation. Do not invent this.>

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

## Step 4 — Approval gate (mandatory)

Present the complete PR to the user for review using `AskUserQuestion` with two
options — **Raise PR** and **Edit** — and use the `preview` field to render the
exact title and body the user will see on GitHub.

Format the preview as:

```
Title: <pr title>

---

<full pr body>
```

Only proceed to Step 5 when the user selects **Raise PR**.
If the user selects **Edit**, ask which part they want to change, update that
part, and re-show the approval gate.

---

## Step 5 — Raise the PR

Once approved, run:

```bash
gh pr create \
  --base <target-branch> \
  --title "<pr-title>" \
  --body "$(cat <<'EOF'
<pr-body>
EOF
)"
```

After creation:
1. Print the PR URL.
2. Remind the user to create the GitHub labels if they do not yet exist (only
   needed once per repo — see the label setup commands in
   [`docs-internal/git-hygiene.md`](../../docs-internal/git-hygiene.md#4-labels)).

---

## Error reference

| Condition | Message |
|---|---|
| Target branch is `master` | "`master` is CI/CD-only. Use `develop`, `hotfix/<version>`, or `release/<version>`." |
| Title too short | "PR title too short (N chars, min 20). Write a descriptive title — it becomes the changelog entry." |
| Title too long | "PR title too long (N chars, max 72). Shorten it to keep the changelog readable." |
| ClickUp ID in title | "Remove the ClickUp ticket ID from the title. It belongs in the ClickUp Ticket section of the body." |
| No upstream for branch | "Branch has no upstream. Run `git push -u origin <branch>` first." |
| `gh` not authenticated | "GitHub CLI is not authenticated. Run `gh auth login` and retry." |
