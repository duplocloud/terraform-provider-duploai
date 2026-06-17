#!/bin/bash -eu
#
# Shared helpers for the three-tier-example driver scripts.
# Sourced by plan.sh / apply.sh / destroy.sh — never run directly.
#
# Design mirrors the tenant-terraform-generator scripts (_util.sh / _env.sh +
# per-action entrypoints), adapted to this example's tier layout
# (infra/ -> app/ -> services/) and local backend.

# ── OS detection (parity with the generator scripts) ───────────────────────
case "$(uname -s)" in
Darwin) export TOOL_OS="darwin" ;;
Linux)  export TOOL_OS="linux" ;;
esac

# ── logging / error helpers ────────────────────────────────────────────────
err() { echo "$0:" "$@" 1>&2 ; }
die() { err "$@" ; exit 1 ; }
logged() { echo "+ $*" 1>&2 ; "$@" ; }

# ── paths ──────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"   # the three-tier-example/ root
export SCRIPT_DIR ROOT

# Auto-load a local .env (copied from .env.example) if present, so creds and
# knobs don't have to be exported by hand. `set -a` auto-exports every
# assignment; bash handles inline `# comments`. .env is git-ignored.
if [ -f "$ROOT/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  . "$ROOT/.env"
  set +a
fi

# Dependency order. apply/plan walk it forwards; destroy walks it in reverse.
TIERS=( infra app services )

usage() {
  die "usage:

  $0 [TIER] [TF_FLAGS...]
  $0 [TIER] -- [TF_FLAGS...]

  TIER        optional — one of: ${TIERS[*]}. Omitted => all tiers, in order
              (destroy runs them in reverse).
  TF_FLAGS    any extra flags passed straight through to terraform, e.g.
              -refresh=false  -target=duploai_node_group.this  -var=region=us-east-1
              Use a leading '--' if a flag could be mistaken for a tier name.

  Required env:
    duplo_host    e.g. https://your-portal.duplocloud.net
    duplo_token   bearer token (or install 'duplo-jit' for interactive login)

  Optional env:
    ssl_no_verify   true|false           (default false)
    var_file        path to a .tfvars/.tfvars.json applied to EVERY tier
    TF_PARALLELISM  terraform -parallelism (default 1, matches the generator)
    AUTO_APPROVE    1 => skip the destroy confirmation prompt

  Per-tier var files are auto-loaded when present:
    config/<tier>.tfvars       (or .tfvars.json)
"
}

# ── argument parsing ───────────────────────────────────────────────────────
# Sets SELECTION (single tier or "") and TF_EXTRA[] (passthrough terraform flags).
SELECTION=""
TF_EXTRA=()
parse_args() {
  local seen_dashes=""
  while [ $# -gt 0 ]; do
    case "$1" in
      -h|--help) usage ;;
      --) seen_dashes=1 ; shift ; TF_EXTRA+=( "$@" ) ; break ;;
      *)
        if [ -z "$seen_dashes" ] && [ -z "$SELECTION" ] && _is_tier "$1"; then
          SELECTION="$1"
        else
          TF_EXTRA+=( "$1" )
        fi
        shift
        ;;
    esac
  done
}

_is_tier() {
  local t
  for t in "${TIERS[@]}"; do [ "$1" = "$t" ] && return 0 ; done
  return 1
}

# ── credential resolution ──────────────────────────────────────────────────
# Explicit duplo_token wins. When run interactively without one, fall back to
# duplo-jit (same behaviour as the generator scripts).
resolve_token() {
  if [ -t 0 ]; then
    if [ -z "${duplo_token:-}" ]; then
      if command -v duplo-jit &>/dev/null; then
        duplo_token="$(duplo-jit duplo --host "${duplo_host:?duplo_host must be set}" --interactive | jq -r '.DuploToken')"
      else
        die "duplo-jit not found and duplo_token not set"
      fi
    fi
  else
    if [ -z "${duplo_token:-}" ]; then
      die "error: duplo_token: environment variable missing or empty"
    fi
  fi
}

# ── terraform wrappers ─────────────────────────────────────────────────────
tf()      { logged terraform "$@" ; }
tf_init() { tf init -input=false "$@" ; }

abspath() { (cd "$(dirname "$1")" && printf '%s/%s\n' "$(pwd)" "$(basename "$1")") ; }

# run_tier ACTION TIER [extra terraform flags...]
run_tier() {
  local action="$1" ; shift
  local tier="$1"   ; shift
  local dir="$ROOT/$tier"
  [ -d "$dir" ] || die "internal error: no such tier directory: $dir"

  # Skip when a single tier was selected and it isn't this one.
  if [ -n "$SELECTION" ] && [ "$SELECTION" != "$tier" ]; then
    return 0
  fi

  local args=( -input=false "-parallelism=${TF_PARALLELISM:-1}" )
  case "$action" in
    apply)   args+=( -auto-approve ) ;;
    destroy) [ "${AUTO_APPROVE:-}" = "1" ] && args+=( -auto-approve ) ;;
  esac

  # Var files: per-tier (config/<tier>.tfvars[.json]), then a global var_file.
  local vf
  for vf in "$ROOT/config/$tier.tfvars" "$ROOT/config/$tier.tfvars.json" "${var_file:-}"; do
    [ -n "$vf" ] && [ -f "$vf" ] && args+=( "-var-file=$(abspath "$vf")" )
  done

  # Caller-supplied passthrough flags last, so they can override the above.
  args+=( "$@" )

  echo "==> terraform $action [$tier]" 1>&2
  ( cd "$dir" && tf_init && tf "$action" "${args[@]}" )
}
