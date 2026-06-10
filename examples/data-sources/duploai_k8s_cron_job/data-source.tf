# Look up a Kubernetes cron job by ID.
data "duploai_k8s_cron_job" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_k8s_cron_job.example.status
}

output "last_successful_time" {
  value = data.duploai_k8s_cron_job.example.last_successful_time
}
