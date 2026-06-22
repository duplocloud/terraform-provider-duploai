# Look up a persona by ID.
data "duploai_admin_persona" "example" {
  id = "<object-id>"
}

output "name" {
  value = data.duploai_admin_persona.example.name
}

output "skill_ids" {
  value = data.duploai_admin_persona.example.skill_ids
}
