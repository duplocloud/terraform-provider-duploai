#!/bin/bash -eu
#
# terraform import for a single tier. Wires DuploCloud creds and the tier's
# var file the same way plan/apply/destroy do. See ../README.md for usage.
#
#   ./scripts/import.sh <tier> <resource_address> <id> [extra terraform flags]
#
# Example:
#   ./scripts/import.sh infra duploai_network_baseline.this \
#       69c13422c25d7d1dc686defa/6a32515fbb0c8056fab50422

# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/_util.sh"
parse_args "$@"
# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/_env.sh"

[ -n "$SELECTION" ] || die "first argument must be a tier (one of: ${TIERS[*]})"
[ "${#TF_EXTRA[@]}" -ge 2 ] || \
  die "usage: $0 <tier> <resource_address> <id> [extra terraform flags]"

# TF_EXTRA holds <resource_address> <id> [flags] (the tier was consumed as
# SELECTION). run_tier adds -input=false and the tier's -var-file, then appends
# these — terraform import wants: import [options] ADDR ID.
run_tier import "$SELECTION" ${TF_EXTRA[@]+"${TF_EXTRA[@]}"}
