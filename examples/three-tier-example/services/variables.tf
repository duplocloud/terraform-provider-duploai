# ── services tier inputs (edit values here for this tier only) ──
# Identity (workspace_id, scope_ids, region, name_prefix) and the tenant/namespace
# ids are NOT set here — they flow in automatically from the app tier's state.
variable "duplo_host" {
  type = string
}
variable "duplo_token" {
  type      = string
  sensitive = true
}
variable "ssl_no_verify" {
  type = bool
}

# App-layer database credentials (used by both the secret and backend env).
variable "db_master_password" {
  type      = string
  sensitive = true
}
