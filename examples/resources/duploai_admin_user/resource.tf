resource "duploai_admin_user" "example" {
  name  = "Alice Example"
  email = "alice@example.com"
  roles = ["user"]
}
