# A gp3 storage class backed by the AWS EBS CSI driver, expandable and
# bound only once a Pod that uses the claim is scheduled.
resource "duploai_k8s_storage_class" "gp3" {
  workspace_id      = "<workspace-id>"
  name              = "gp3"
  environment_id    = "<environment-id>"
  resource_group_id = "<eks-resource-group-id>"

  storage_provisioner = "ebs.csi.aws.com"
  parameters = {
    type      = "gp3"
    encrypted = "true"
  }

  reclaim_policy         = "Delete"
  volume_binding_mode    = "WaitForFirstConsumer"
  allow_volume_expansion = true

  # Mark this class as the cluster default (optional).
  annotations = {
    "storageclass.kubernetes.io/is-default-class" = "true"
  }

  labels = {
    app = "myapp"
  }

  timeouts {
    create = "15m"
    delete = "10m"
  }
}
