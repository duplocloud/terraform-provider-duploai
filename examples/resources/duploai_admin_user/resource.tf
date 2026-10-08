resource "duploai_admin_user" "example" {
  name  = "Alice Example"
  email = "alice@example.com"

  # Exactly one role: "Administrator" or "User" (case-sensitive). "User" is the default when unset.
  roles = ["User"]
}
