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

# Job that opts out of the Resource-Group nodeSelector. By default a job's pods
# are pinned to nodes labeled for its resource group (key "resourcegroup") — if
# that resource group has no dedicated node group, the pods stay Pending
# forever. Set is_any_host_allowed = true to let the pods schedule on any node
# instead. allocation_tags further narrows that to nodes whose node group was
# provisioned with a matching allocation tag.
resource "duploai_k8s_job" "any_host" {
  workspace_id      = "<workspace-id>"
  name              = "shared-cleanup"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "default"

  is_any_host_allowed = true
  allocation_tags     = "shared-pool"

  containers = [
    {
      name  = "cleanup"
      image = "my-repo/cleanup:latest"
    },
  ]
}

# Full example — Indexed completions, pod failure policy, private-registry pull
# secret, volumes + mounts, secret/configMap-sourced env, resource limits, probes,
# init container, and scheduling. Note: restart_policy must be "Never" when
# pod_failure_policy is set, and k8s_job is immutable (no "update" timeout).
resource "duploai_k8s_job" "full" {
  workspace_id      = "<workspace-id>"
  name              = "nightly-report"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
  namespace_name    = "jobs"

  provisioner_type = "IacNativeTf"

  # ---- Job spec ----
  completions                = 5
  parallelism                = 2
  completion_mode            = "Indexed"
  backoff_limit              = 2
  backoff_limit_per_index    = 1
  active_deadline_seconds    = 3600
  ttl_seconds_after_finished = 86400

  pod_failure_policy = {
    rules = [
      { action = "FailJob", on_exit_codes = { operator = "In", values = [1, 42] } },
      { action = "Ignore", on_pod_conditions = [{ type = "DisruptionTarget", status = "True" }] },
    ]
  }

  # ---- Pod spec ----
  restart_policy       = "Never" # required when pod_failure_policy is set
  service_account_name = "report-runner"
  node_selector        = { "node-role" = "batch" }
  labels               = { app = "nightly-report", team = "data-platform" }

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
    { name = "creds", secret = { secret_name = "report-secret", optional = true } },
  ]

  init_containers = [
    {
      name               = "init"
      image              = "busybox:1.36"
      command            = ["sh", "-c", "echo preparing"]
      volume_mounts      = [{ name = "scratch", mount_path = "/data" }]
      resources_requests = { cpu = "50m", memory = "32Mi" }
    }
  ]

  containers = [
    {
      name              = "reporter"
      image             = "my-repo/report-generator:2.0.1"
      image_pull_policy = "IfNotPresent"
      args              = ["--output=s3://my-bucket/reports/", "--date=yesterday"]

      env = [
        { name = "AWS_REGION", value = "us-east-1" },
        { name = "DB_PASSWORD", value_from = { secret_key_ref = { name = "report-secret", key = "password" } } },
        { name = "FLAGS", value_from = { config_map_key_ref = { name = "report-config", key = "flags", optional = true } } },
        { name = "POD_IP", value_from = { field_ref = { field_path = "status.podIP" } } },
      ]
      env_from = [
        { config_map_ref = { name = "report-config", optional = true } },
      ]

      resources_requests = { cpu = "250m", memory = "256Mi" }
      resources_limits   = { cpu = "1", memory = "512Mi" }

      volume_mounts = [
        { name = "scratch", mount_path = "/data" },
        { name = "config", mount_path = "/etc/config", read_only = true },
        { name = "creds", mount_path = "/etc/creds", read_only = true },
      ]

      liveness_probe = {
        exec           = { command = ["sh", "-c", "test -f /data/alive"] }
        period_seconds = 30
      }

      security_context = {
        run_as_user                = 1000
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
