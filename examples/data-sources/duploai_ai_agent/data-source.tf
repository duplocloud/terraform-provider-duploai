# Look up an AI agent by ID.
data "duploai_ai_agent" "example" {
  id = "<object-id>"
}

output "name" {
  value = data.duploai_ai_agent.example.name
}

output "endpoint" {
  value = data.duploai_ai_agent.example.endpoint
}

output "is_active" {
  value = data.duploai_ai_agent.example.is_active
}
