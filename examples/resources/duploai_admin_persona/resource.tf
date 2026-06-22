# Persona with a system prompt and assigned skills.
resource "duploai_admin_persona" "devops" {
  name        = "devops-assistant"
  description = "Assists with DevOps tasks"
  prompt_md   = "You are a DevOps assistant. Help users manage infrastructure safely."

  skill_ids = ["<skill-id-1>", "<skill-id-2>"]
}

# Minimal persona — name only.
resource "duploai_admin_persona" "basic" {
  name = "general-assistant"
}
