# Daily USD quota.
resource "duploai_admin_quota_definition" "daily" {
  name      = "daily-spend"
  type      = "Daily"
  limit_usd = 50
}

# Monthly USD quota.
resource "duploai_admin_quota_definition" "monthly" {
  name        = "monthly-budget"
  type        = "Monthly"
  limit_usd   = 2000
  description = "Monthly org budget"
}
