resource "duploai_user" "example" {
  name         = "alice"
  email        = "alice@example.com"
  display_name = "Alice Example"
  roles        = ["user"]
}
