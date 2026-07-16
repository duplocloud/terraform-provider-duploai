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

# Full example — cron scheduling plus the full job-template / pod / container
# config: pod failure policy, private-registry pull secret, volumes + mounts,
# secret/configMap-sourced env, resource limits, probes, and an init container.
# Note: restart_policy must be "Never" when pod_failure_policy is set, and
# k8s_cron_job is immutable (no "update" timeout).
resource "duploai_k8s_cron_job" "full" {
  workspace_id      = "<workspace-id>"
  name              = "weekly-report"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "jobs"

  # ---- CronJob spec ----
  schedule                      = "0 6 * * MON"
  time_zone                     = "America/New_York"
  concurrency_policy            = "Replace"
  suspend                       = true
  starting_deadline_seconds     = 300
  successful_jobs_history_limit = 3
  failed_jobs_history_limit     = 1

  # ---- Job-template spec ----
  completions                = 1
  parallelism                = 1
  completion_mode            = "NonIndexed"
  backoff_limit              = 1
  active_deadline_seconds    = 3600
  ttl_seconds_after_finished = 86400

  pod_failure_policy = {
    rules = [
      { action = "FailJob", on_exit_codes = { operator = "In", values = [1] } }
    ]
  }

  # ---- Pod spec ----
  restart_policy       = "Never" # required when pod_failure_policy is set
  service_account_name = "report-runner"
  node_selector        = { "node-role" = "batch" }
  labels               = { app = "weekly-report", team = "data-platform" }

  image_pull_secrets = [{ name = "regcred" }]

  tolerations = [
    { key = "dedicated", operator = "Equal", value = "batch", effect = "NoSchedule" }
  ]

  pod_security_context = {
    run_as_non_root = true
    fs_group        = 2000
    seccomp_profile = { type = "RuntimeDefault" }
  }

  volumes = [
    { name = "scratch", empty_dir = { medium = "Memory", size_limit = "64Mi" } },
    { name = "config", config_map = { name = "report-config", optional = true } },
  ]

  init_containers = [
    {
      name               = "init"
      image              = "busybox:1.36"
      command            = ["sh", "-c", "echo preparing"]
      resources_requests = { cpu = "50m", memory = "32Mi" }
    }
  ]

  containers = [
    {
      name              = "reporter"
      image             = "my-repo/report-generator:2.0.1"
      image_pull_policy = "IfNotPresent"
      args              = ["--output=s3://my-bucket/reports/", "--range=last-week"]

      env = [
        { name = "AWS_REGION", value = "us-east-1" },
        { name = "DB_PASSWORD", value_from = { secret_key_ref = { name = "report-secret", key = "password" } } },
        { name = "FLAGS", value_from = { config_map_key_ref = { name = "report-config", key = "flags", optional = true } } },
      ]
      env_from = [
        { config_map_ref = { name = "report-config", optional = true } },
      ]

      resources_requests = { cpu = "250m", memory = "256Mi" }
      resources_limits   = { cpu = "1", memory = "512Mi" }

      volume_mounts = [
        { name = "scratch", mount_path = "/data" },
        { name = "config", mount_path = "/etc/config", read_only = true },
      ]

      liveness_probe = {
        exec           = { command = ["sh", "-c", "true"] }
        period_seconds = 30
      }

      security_context = {
        run_as_non_root            = true
        read_only_root_filesystem  = true
        allow_privilege_escalation = false
        capabilities               = { drop = ["ALL"] }
        seccomp_profile            = { type = "RuntimeDefault" }
      }
    },
  ]

  timeouts {
    create = "40m"
    delete = "15m"
  }
}
