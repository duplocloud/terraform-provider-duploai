#!/bin/bash -eu
#
# Validates DuploCloud credentials and surfaces them to terraform.
# Sourced by plan.sh / apply.sh / destroy.sh AFTER _util.sh.
#
# The duploai provider has no env-var fallback — it reads duplo_host / duplo_token
# / ssl_no_verify from provider config (var.* in this example). We therefore
# export them as TF_VAR_* so no secret is ever hand-written into a .tf or
# committed .tfvars file.

resolve_token

for key in duplo_host duplo_token; do
  eval "[ -n \"\${${key}:-}\" ]" || die "error: $key: environment variable missing or empty"
done

export TF_VAR_duplo_host="$duplo_host"
export TF_VAR_duplo_token="$duplo_token"
export TF_VAR_ssl_no_verify="${ssl_no_verify:-false}"
