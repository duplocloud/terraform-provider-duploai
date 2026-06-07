# Minimal example — install a few components onto an existing EKS cluster.
# region, vpc_id, cluster_name, and scope_ids are inherited from the cluster.
resource "duploai_cluster_attributes" "basic" {
  workspace_id = "<workspace-id>"
  name         = "<cluster-attributes-name>"
  cluster_id   = "<cluster-id>"

  components = {
    cluster_autoscaler           = true
    alb_load_balancer_controller = true
    efs_volumes                  = true
    metrics_server               = true
  }
}

# Install components and configure ExternalDNS to manage a specific Route53 zone.
resource "duploai_cluster_attributes" "with_external_dns" {
  workspace_id = "<workspace-id>"
  name         = "<cluster-attributes-name>"
  cluster_id   = "<cluster-id>"

  components = {
    cluster_autoscaler           = true
    alb_load_balancer_controller = true
    efs_volumes                  = false
    metrics_server               = true
    external_dns                 = true
  }

  external_dns_config = {
    provider       = "aws"
    sources        = ["service", "ingress"]
    policy         = "upsert-only"
    txt_owner_id   = "<cluster-attributes-name>"
    domain_filters = ["example.com"]
  }
}

# Full example — linked to a cluster baseline resource, with EKS managed add-ons,
# ExternalDNS, the Secrets Store CSI driver, GitOps via Flux CD, a custom
# provisioner, and extended timeouts.
#
# Available EKS managed add-on names:
#   vpc-cni                      — VPC CNI plugin for pod networking
#   coredns                      — In-cluster DNS
#   kube-proxy                   — Kubernetes network proxy
#   aws-ebs-csi-driver           — EBS persistent volumes          (needs IRSA)
#   aws-efs-csi-driver           — EFS persistent volumes          (needs IRSA)
#   aws-mountpoint-s3-csi-driver — S3 as a filesystem              (needs IRSA)
#   snapshot-controller          — Volume snapshot support
#   eks-pod-identity-agent       — EKS Pod Identity (modern IRSA alternative)
#   amazon-cloudwatch-observability — CloudWatch Container Insights (needs IRSA)
#   adot                         — AWS Distro for OpenTelemetry    (needs IRSA)
#   aws-guardduty-agent          — GuardDuty runtime monitoring
resource "duploai_cluster_baseline" "this" {
  workspace_id = "<workspace-id>"
  name         = "prod-cluster"
  network_id   = "<network-id>"
  eks_version  = "1.34"
}

resource "duploai_cluster_attributes" "full" {
  workspace_id = "<workspace-id>"
  name         = "prod-cluster-attrs"
  cluster_id   = duploai_cluster_baseline.this.id

  provisioner_type = "IacNativeTf"

  components = {
    cluster_autoscaler           = true
    secret_csi_driver            = true
    alb_load_balancer_controller = true
    efs_volumes                  = true
    metrics_server               = true
    kube_state_metrics           = true
    flux_cd                      = true
    external_dns                 = true
  }

  eks_addons = [
    # Core networking and DNS — usually pre-installed; pin versions to control upgrades.
    { name = "vpc-cni", version = "v1.19.0-eksbuild.1" },
    { name = "coredns", version = "v1.11.4-eksbuild.2" },
    { name = "kube-proxy", version = "v1.31.2-eksbuild.3" },

    # Storage — require an IRSA role with the appropriate AWS-managed policy.
    {
      name                     = "aws-ebs-csi-driver"
      version                  = "v1.37.0-eksbuild.1"
      service_account_role_arn = "<ebs-csi-irsa-role-arn>"
    },
    {
      name                     = "aws-efs-csi-driver"
      service_account_role_arn = "<efs-csi-irsa-role-arn>"
    },
    {
      name                     = "aws-mountpoint-s3-csi-driver"
      service_account_role_arn = "<s3-csi-irsa-role-arn>"
    },

    # Volume snapshots.
    { name = "snapshot-controller" },

    # Identity — EKS Pod Identity agent (modern alternative to IRSA).
    { name = "eks-pod-identity-agent" },

    # Observability — require an IRSA role.
    {
      name                     = "amazon-cloudwatch-observability"
      service_account_role_arn = "<cloudwatch-irsa-role-arn>"
    },
    {
      name                     = "adot"
      service_account_role_arn = "<adot-irsa-role-arn>"
    },

    # Security.
    { name = "aws-guardduty-agent" },
  ]

  external_dns_config = {
    provider       = "aws"
    sources        = ["service", "ingress"]
    policy         = "upsert-only"
    txt_owner_id   = "prod-cluster-attrs"
    domain_filters = ["prod.example.com"]
  }

  timeouts {
    create = "45m"
    update = "30m"
    delete = "20m"
  }
}
