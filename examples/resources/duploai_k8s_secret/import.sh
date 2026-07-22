# Import an existing secret resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - SECRET_ID is the unique identifier of the secret (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_k8s_secret.app WORKSPACE_ID/SECRET_ID
# Example:
# terraform import duploai_k8s_secret.app 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
#
# NOTE: scope_ids, provisioner_version, and description are write-only — the
# API never returns them, so import leaves them empty in state. Add the
# correct values to your config and run `terraform apply` once after import
# to populate them.
