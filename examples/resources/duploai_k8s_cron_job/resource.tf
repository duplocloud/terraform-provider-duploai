# Minimal example — a single-container cron job that runs a shell command nightly.
resource "duploai_k8s_cron_job" "basic" {
  workspace_id      = "<workspace-id>"
  name              = "nightly-cleanup"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "default"

  schedule = "0 2 * * *"

  containers = [
    {
      name    = "cleaner"
      image   = "my-repo/db-cleaner:v1.0.0"
      command = ["/bin/sh"]
      args    = ["-c", "python cleanup.py --older-than 30d"]
      env = [
        { name = "DB_HOST", value = "<db-host>" },
        { name = "DB_PASSWORD", value = "<db-password>" },
      ]
    },
  ]
}

# Cron job with concurrency and history controls — skip a run if the previous
# one is still going, and keep a short history of finished runs.
resource "duploai_k8s_cron_job" "report" {
  workspace_id      = "<workspace-id>"
  name              = "hourly-sync"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "default"

  schedule           = "@hourly"
  concurrency_policy = "Forbid"

  successful_jobs_history_limit = 5
  failed_jobs_history_limit     = 2
  backoff_limit                 = 2

  # Clean up each finished run 10 minutes after completion.
  ttl_seconds_after_finished = 600

  containers = [
    {
      name  = "sync"
      image = "my-repo/data-sync:latest"
      env = [
        { name = "SOURCE_URL", value = "<source-url>" },
      ]
    },
  ]
}

# Full example — time zone, deadlines, custom service account, node selector,
# labels, annotations, and extended timeouts. Created suspended so the first
# run only happens after the cron job is unsuspended.
resource "duploai_k8s_cron_job" "full" {
  workspace_id      = "<workspace-id>"
  name              = "weekly-report"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "jobs"

  schedule           = "0 6 * * MON"
  time_zone          = "America/New_York"
  concurrency_policy = "Replace"
  suspend            = true

  # A missed run may still start up to 5 minutes after its scheduled time.
  starting_deadline_seconds = 300

  # Pin pods to hosts carrying these allocation tags (or allow any host).
  is_any_host_allowed = false
  allocation_tags     = "batch-workers"

  backoff_limit              = 1
  active_deadline_seconds    = 3600
  ttl_seconds_after_finished = 86400
  restart_policy             = "Never"
  service_account_name       = "report-runner"

  node_selector = {
    "node-role" = "batch"
  }

  labels = {
    "app"  = "weekly-report"
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
      args  = ["--output=s3://my-bucket/reports/", "--range=last-week"]
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
