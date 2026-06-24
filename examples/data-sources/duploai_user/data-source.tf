data "duploai_user" "example" {
  id = "<user-id>"
}

output "user_email" {
  value = data.duploai_user.example.email
}
