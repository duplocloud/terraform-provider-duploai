#!/bin/bash -eu
#
# terraform plan across the three tiers (infra -> app -> services).
# See ../README.md for usage.

# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/_util.sh"
parse_args "$@"
# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/_env.sh"

for tier in "${TIERS[@]}"; do
  run_tier plan "$tier" ${TF_EXTRA[@]+"${TF_EXTRA[@]}"}
done
