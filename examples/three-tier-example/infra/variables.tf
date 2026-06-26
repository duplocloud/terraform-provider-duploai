# ── infra tier inputs (edit values here for this tier only) ──
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
variable "workspace_id" {
  type = string
}
variable "scope_ids" {
  type = list(string)
}
variable "region" {
  type = string
}
variable "name_prefix" {
  type = string
}
variable "eks_version" {
  type    = string
  default = "1.35"
}
