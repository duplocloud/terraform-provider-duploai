# Minimal example — a single-container job that runs a shell command to completion.
resource "duploai_k8s_job" "basic" {
  workspace_id      = "<workspace-id>"
  name              = "data-migration"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "default"

  containers = [
    {
      name    = "migrator"
      image   = "my-repo/db-migrator:v1.2.0"
      command = ["/bin/sh"]
      args    = ["-c", "python migrate.py --env prod"]
      env = [
        { name = "DB_HOST", value = "<db-host>" },
        { name = "DB_PASSWORD", value = "<db-password>" },
      ]
    },
  ]
}

# Job with parallelism — fan out work across multiple pods.
resource "duploai_k8s_job" "parallel" {
  workspace_id      = "<workspace-id>"
  name              = "batch-processor"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "default"

  completions   = 10
  parallelism   = 3
  backoff_limit = 2

  # Automatically clean up 10 minutes after the job finishes.
  ttl_seconds_after_finished = 600

  containers = [
    {
      name  = "worker"
      image = "my-repo/batch-worker:latest"
      env = [
        { name = "QUEUE_URL", value = "<sqs-queue-url>" },
      ]
    },
  ]
}

# Full example — custom service account, node selector, labels, annotations,
# deadline, and extended timeouts.
resource "duploai_k8s_job" "full" {
  workspace_id      = "<workspace-id>"
  name              = "nightly-report"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "jobs"

  provisioner_type = "IacNativeTf"

  backoff_limit              = 1
  active_deadline_seconds    = 3600
  ttl_seconds_after_finished = 86400
  restart_policy             = "Never"
  service_account_name       = "report-runner"

  node_selector = {
    "node-role" = "batch"
  }

  labels = {
    "app"  = "nightly-report"
    "team" = "data-platform"
    "env"  = "prod"
  }

  annotations = {
    "prometheus.io/scrape" = "false"
  }

  containers = [
    {
      name  = "reporter"
      image = "my-repo/report-generator:2.0.1"
      args  = ["--output=s3://my-bucket/reports/", "--date=yesterday"]
      env = [
        { name = "AWS_REGION", value = "us-east-1" },
        { name = "REPORT_FORMAT", value = "pdf" },
        { name = "NOTIFY_SLACK", value = "true" },
      ]
    },
  ]

  timeouts {
    create = "40m"
    update = "20m"
    delete = "15m"
  }
}
