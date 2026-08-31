# Minimal workspace — the required name, plus at least one persona and one scope.
resource "duploai_admin_workspace" "basic" {
  name = "support-team"

  persona_ids = [
    "<persona-id>",
  ]
  scope_ids = [
    "<scope-id>",
  ]
}

# Full workspace — labels, a system prompt, a quota, and persona/scope links.
resource "duploai_admin_workspace" "full" {
  name        = "platform-support"
  short_name  = "PLAT"
  description = "Platform support workspace"
  email       = "platform-support@example.com"
  role        = "support"
  team        = "platform"
  prompt_md   = "You are a DevOps assistant for the platform team. Help users manage infrastructure safely."

  quota_id = "<quota-id>"

  persona_ids = [
    "<persona-id>",
  ]
  scope_ids = [
    "<scope-id>",
  ]

  meta_data = {
    tier   = "gold"
    region = "us-east-1"
  }
}

# Workspace with prompt suggestions and prompt templates. These are stored as
# workspace metadata: each is a list of objects that the platform persists as a
# JSON-encoded string, so author them as native HCL and wrap with jsonencode().
# scopeIds reference existing scopes; permission scopes which commands the prompt
# may run (approved/rejected command regexes).
resource "duploai_admin_workspace" "with_prompts" {
  name        = "prompt-workspace"
  description = "Workspace with curated prompt suggestions and templates"

  persona_ids = [
    "<persona-id>",
  ]
  scope_ids = [
    "<scope-id>",
  ]

  meta_data = {
    prompt_suggestions = jsonencode([
      {
        text     = "Show error rate for the payments service"
        scopeIds = ["<scope-id>"]
        permission = {
          approvedCmdRegEx = [".*"]
          rejectedCmdRegEx = ["kubectl delete .*"]
        }
      },
    ])

    prompt_templates = jsonencode([
      {
        name        = "incident-summary"
        content     = "Summarize the incident, list impacted services, and propose remediation steps."
        description = "Standard incident triage template"
        scopeIds    = ["<scope-id>"]
        permission = {
          approvedCmdRegEx = ["kubectl get .*", "kubectl describe .*"]
          rejectedCmdRegEx = [".*delete.*"]
        }
      },
    ])
  }
}
