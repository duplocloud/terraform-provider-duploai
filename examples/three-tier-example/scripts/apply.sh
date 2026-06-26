#!/bin/bash -eu
#
# terraform apply across the three tiers, in dependency order
# (infra -> app -> services). Auto-approved. See ../README.md for usage.

# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/_util.sh"
parse_args "$@"
# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/_env.sh"

for tier in "${TIERS[@]}"; do
  run_tier apply "$tier" ${TF_EXTRA[@]+"${TF_EXTRA[@]}"}
done
