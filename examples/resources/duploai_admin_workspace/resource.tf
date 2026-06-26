# Minimal workspace — only the required name.
resource "duploai_admin_workspace" "basic" {
  name = "support-team"
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

  quota_id = "6a2258e94703bc957a1b824e"

  persona_ids = [
    "69f1bc7e527b7c8f48e321a9",
  ]
  scope_ids = [
    "6a25105705686d697e0da225",
  ]

  meta_data = {
    tier   = "gold"
    region = "us-east-1"
  }
}
