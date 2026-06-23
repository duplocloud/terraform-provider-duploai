# Look up a skill by ID.
data "duploai_admin_skill" "example" {
  id = "<object-id>"
}

output "name" {
  value = data.duploai_admin_skill.example.name
}

output "format" {
  value = data.duploai_admin_skill.example.format
}
