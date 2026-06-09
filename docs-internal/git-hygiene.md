# Git Hygiene & PR Workflow

This document covers everything needed to raise a pull request, get it merged, and
see it appear correctly in the published release changelog. Read it once before
opening your first PR.

---

## 1. Branch Strategy (GitFlow)

```
master          production — only CI/CD merges here (release/* and hotfix/*)
  └── release/X.Y.Z    short-lived; created by "Start Release" workflow
develop         integration — all feature/fix work targets this branch
  └── DUPLOAI-1234-short-description   your working branch
hotfix/X.Y.Z    emergency patch from master; created by "Start Hotfix" workflow
```

**Rules enforced by branch protection:**
- `master` accepts merges only from `release/*` and `hotfix/*` branches (CI/CD-only).
- Manual PRs must target `develop`, `release/*`, or `hotfix/*`.
- Never push directly to `master` or `develop`.

### Branch Naming

Use your ClickUp ticket ID followed by a short lowercase description:

```
DUPLOAI-1618-fix-endpoint-timeout
DUPLOAI-1234-add-s3-bucket-resource
DUPLOAI-9999-update-waiter-logic
```

The ticket ID in the branch name is for your own tracking. It does **not** appear
in the changelog — only the PR title does.

---

## 2. PR Title Rules

PR titles become changelog entries verbatim. Write them as if writing release notes
for a customer reading the GitHub Release page.

| Rule | Limit | Reason |
|---|---|---|
| Minimum length | 20 chars | Forces a real description |
| Maximum length | 72 chars | Stays readable on one line in the changelog |
| No ClickUp ID | — | Internal reference; meaningless to users |

**Good titles:**
```
Add S3 bucket resource with versioning support
Fix endpoint connection timeout on retry
Update waiter default timeout to 30 minutes
```

**Bad titles:**
```
DUPLOAI-1618: fix bug          ← too short + has ticket ID
Fix                            ← too short
DUPLOAI-1234 Added the new S3 bucket resource implementation with all the configuration options including versioning, encryption, and lifecycle rules   ← too long + has ticket ID
```

**Auto-strip:** If your PR title contains `DUPLOAI-XXXXX`, the CI bot strips it
automatically and updates the title. You will see a warning in the workflow run.

---

## 3. PR Template — Required Sections

Every PR must use the template. The `PR Validation` workflow enforces all sections.

| Section | What to fill in |
|---|---|
| **ClickUp Ticket** | Ticket ID (`DUPLOAI-12345`) or full URL. Required in the body, not the title. |
| **Type** | Check exactly one label (see [Labels](#4-labels) below). |
| **Overview** | One or two sentences on *why* this change exists. |
| **Summary of changes** | Bullet list of what changed. |
| **Testing performed** | Check all test types you ran. |
| **Describe any breaking changes** | List any breaking changes, or write "None". |

Leaving any section empty or unchecked will fail the `PR Validation` check.

---

## 4. Labels

Labels drive the changelog. The `Label PR` workflow reads the **Type** checkbox you
check in the PR template and applies the corresponding GitHub label automatically.
You never need to apply labels manually.

| Checkbox | Label | Changelog section |
|---|---|---|
| `enhancement` | `enhancement` | New Features |
| `bug` | `bug` | Bug Fixes |
| `breaking-change` | `breaking-change` | Breaking Changes |
| `documentation` | `documentation` | Documentation |
| *(none checked)* | *(none)* | Other Changes |

PRs with no matching label (e.g. release/hotfix automation PRs) fall into
**Other Changes**, which is excluded from the main changelog sections.

Two special labels exist for filtering:

| Label | Effect |
|---|---|
| `ignore-for-release` | Excluded entirely from the changelog |
| `dependencies` | Excluded entirely from the changelog |

---

## 5. Checks That Run on Every PR

| Workflow | What it checks | Blocks merge? |
|---|---|---|
| `PR Validation` | Title length, no ClickUp ID in title, all sections present, ClickUp ID in body, exactly one Type checked | Yes |
| `Label PR` | Reads Type checkbox, applies/swaps GitHub label | No (informational) |
| `CI` | `go build`, `go test ./...` | Yes |
| `Lint` | `gofmt`, `golangci-lint` | Yes |
| `Generate` | Docs and examples are up to date (`make doc`) | Yes |
| `CodeQL` | Security scanning | Yes (on protected branches) |

Acceptance tests (`TF_ACC=1`) do **not** run automatically. Add the label
`run-acceptance` to your PR to trigger them, or run them locally with `make testacc`.

---

## 6. How the Changelog Is Generated

The changelog is built automatically at release time — no one writes it by hand.

### Source of truth: PR titles

When a release tag (`v*`) is pushed, GoReleaser runs and calls the GitHub API
to collect all merged PRs since the previous tag. Each PR title becomes one
changelog line, grouped by its label into the sections defined in
[`.github/release.yml`](../.github/release.yml).

```
v0.2.0 changelog
────────────────────────────────────────────
Breaking Changes
• Rename duploai_service to duploai_ecs_service

New Features
• Add S3 bucket resource with versioning support
• Add RDS instance resource

Bug Fixes
• Fix endpoint connection timeout on retry
• Fix waiter not respecting custom timeout

Documentation
• Add architecture diagram to docs-internal
────────────────────────────────────────────
```

Because titles are the changelog, the title quality rules in [Section 2](#2-pr-title-rules)
directly affect the quality of what customers read.

### GoReleaser config

Relevant snippet from [`.goreleaser.yaml`](../.goreleaser.yaml):

```yaml
changelog:
  use: github-native   # delegate to GitHub's release notes API
```

The `github-native` mode means GoReleaser does not parse git commits at all.
Commit message format (conventional commits, etc.) has no effect on the changelog.

### Changelog categories config

Defined in [`.github/release.yml`](../.github/release.yml). Categories are matched
in order; a PR label matches the first category whose labels list includes it.
PRs with no matching label appear in **Other Changes**.

---

## 7. Release Flow

### Normal release

```
1. Trigger "Start Release" workflow (manual dispatch on GitHub Actions)
       → creates release/X.Y.Z branch from develop
       → bumps VERSION in Makefile, regenerates docs, updates examples

2. Review the auto-created PR: release/X.Y.Z → master
       → run any final checks

3. Merge PR to master
       → "Finish Release" workflow fires automatically
       → creates git tag vX.Y.Z
       → bumps minor version on develop (patch resets to 0)

4. Tag push triggers "Release Publish" workflow
       → GoReleaser builds cross-platform binaries
       → Signs SHA256SUMS with GPG
       → Creates a DRAFT GitHub Release with changelog

5. Reviewer approves draft release (manual step in GitHub UI)
       → Release goes live on GitHub and Terraform Registry
       → Slack notification sent
```

### Hotfix

```
1. Trigger "Start Hotfix" workflow (patch version auto-incremented)
       → creates hotfix/X.Y.Z branch from master

2. Apply fix, push commits to hotfix/X.Y.Z

3. Merge PR to master
       → "Finish Hotfix" workflow fires, creates tag
       → same publish flow as above
```

---

## 8. Quick Reference Checklist

Before opening a PR:

- [ ] Branch named `DUPLOAI-XXXX-short-description`
- [ ] PR title is 20–72 chars with no ClickUp ID
- [ ] ClickUp Ticket section filled in the template
- [ ] Exactly one Type checkbox checked
- [ ] Overview and Summary of changes written (not left as `...`)
- [ ] Testing performed checkboxes match what you actually ran
- [ ] Breaking changes listed (or "None")
- [ ] `make vet && make build` passes locally
- [ ] `make doc` run if you changed any resource specs or examples
