# Command policy: auto-approve safe read-only commands, always deny destructive ones.
# The policy has no effect until a command policy mapping binds it to a scope
# (System, Workspace, or Project).
resource "duploai_admin_command_policy" "read_only" {
  name        = "read-only-safe"
  description = "Auto-approve read-only inspection commands; deny anything destructive"

  allow_list = [
    "^kubectl (get|describe) .*",
    "^aws .* (describe|list|get)-.*",
  ]

  block_list = [
    "delete",
    "rm -rf",
    "drop database",
    "terraform destroy",
  ]
}

# Minimal policy — name only (empty allow/block lists; nothing auto-decided yet).
resource "duploai_admin_command_policy" "empty" {
  name = "baseline"
}
