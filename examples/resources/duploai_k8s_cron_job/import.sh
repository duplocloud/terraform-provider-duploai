# Import an existing Kubernetes cron job resource.
#  - WORKSPACE_ID     is the ID of the workspace (e.g. 6a1578ae322a8a4142bbfa04)
#  - K8S_CRON_JOB_ID  is the ID of the k8s cron job resource (e.g. 6a2400004703bc957a000001)
terraform import duploai_k8s_cron_job.basic WORKSPACE_ID/K8S_CRON_JOB_ID
# Example:
# terraform import duploai_k8s_cron_job.basic 6a1578ae322a8a4142bbfa04/6a2400004703bc957a000001
