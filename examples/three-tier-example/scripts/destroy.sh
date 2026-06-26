#!/bin/bash -eu
#
# terraform destroy across the three tiers, in REVERSE dependency order
# (services -> app -> infra). Prompts per tier unless AUTO_APPROVE=1.
# See ../README.md for usage.

# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/_util.sh"
parse_args "$@"
# shellcheck disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/_env.sh"

# Walk the tiers in reverse so dependents are torn down before their providers.
for (( i=${#TIERS[@]}-1; i>=0; i-- )); do
  run_tier destroy "${TIERS[$i]}" ${TF_EXTRA[@]+"${TF_EXTRA[@]}"}
done
