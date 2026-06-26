resource "duploai_app_service" "backend" {
  workspace_id      = local.workspace_id
  name              = "backend"
  scope_ids         = local.scope_ids
  resource_group_id = local.resource_group_id
  environment_id    = local.environment_id
  namespace_name    = local.namespace_name
  deployment_name   = "backend"
  replicas          = 1

  containers = [
    {
      name  = "backend"
      image = "public.ecr.aws/w8y4p9l8/demoapp-backend:v3"
      ports = [{ container_port = 3000, name = "http", protocol = "TCP" }]
      # app_service has no envFrom/secretRef, so DB creds are passed as inline
      # env (mirrors the `db` secret). Host filled from the DB endpoint.
      env = [
        { name = "DB_HOST", value = "<db-endpoint>" },
        { name = "DB_PORT", value = "3306" },
        { name = "DB_USER", value = "appadmin" },
        { name = "DB_PASSWORD", value = var.db_master_password },
        { name = "DB_NAME", value = "app" },
      ]
    },
  ]

  service = {
    name  = "backend"
    type  = "ClusterIP"
    ports = [{ port = 3000, target_port = 3000, protocol = "TCP", name = "http" }]
  }

  depends_on = [duploai_k8s_secret.db]
}
