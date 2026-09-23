# Minimal Git repository — a public repo tracked on a branch.
resource "duploai_git_repository" "podinfo" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "flux-system"

  name       = "podinfo"
  url        = "https://github.com/stefanprodan/podinfo.git"
  ref_branch = "master"
  interval   = "1m"
}

# Private repository over HTTPS. One secret carries every credential kind Flux
# needs — basic-auth username/password, SSH identity + known_hosts, or a CA
# certificate — so there is no separate cert or service-account field.
resource "duploai_git_repository" "private" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "flux-system"

  name            = "private-app"
  url             = "https://github.com/acme/private-app.git"
  ref_branch      = "main"
  secret_ref_name = "git-credentials"
  interval        = "5m"
}

# Pinned to an exact revision. Only one ref_* attribute may be set at a time —
# the provider rejects a second one at plan time.
resource "duploai_git_repository" "pinned" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "flux-system"

  name       = "pinned-release"
  url        = "https://github.com/acme/platform.git"
  ref_commit = "9fdc2a1b3e4d5f60718293a4b5c6d7e8f9012345"
}

# Release tracking by semver range, over SSH, with signed-tag verification.
resource "duploai_git_repository" "verified" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "flux-system"

  name            = "platform-releases"
  url             = "ssh://git@github.com/acme/platform.git"
  ref_semver      = ">=1.0.0 <2.0.0"
  secret_ref_name = "git-ssh-key"

  # Verify the signature on the tag Flux checks out.
  verify_mode            = "Tag"
  verify_secret_ref_name = "git-pgp-public-keys"

  timeout = "60s"
}

# Large monorepo — check out only what is needed, skip submodules, and exclude
# extra paths from the produced artifact.
resource "duploai_git_repository" "monorepo" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "flux-system"

  name       = "monorepo"
  url        = "https://github.com/acme/monorepo.git"
  ref_branch = "main"

  sparse_checkout = [
    "clusters/production",
    "apps/base",
  ]

  recurse_submodules = false
  ignore             = "/docs/\n/*.md"

  labels = {
    team = "platform"
  }

  annotations = {
    "example.com/owner" = "platform-team"
  }
}

# Assemble one artifact from several repositories. Each include overlays another
# GitRepository's content at the given path.
resource "duploai_git_repository" "composed" {
  workspace_id      = "<workspace-id>"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "flux-system"

  name       = "composed-config"
  url        = "https://github.com/acme/cluster-config.git"
  ref_branch = "main"

  include = [
    {
      repository_name = duploai_git_repository.podinfo.name
      from_path       = "kustomize"
      to_path         = "vendor/podinfo"
    },
    {
      repository_name = duploai_git_repository.private.name
    },
  ]

  timeouts {
    create = "15m"
    update = "15m"
    delete = "10m"
  }
}
