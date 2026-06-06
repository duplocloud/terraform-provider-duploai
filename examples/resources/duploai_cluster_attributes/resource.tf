# Install core cluster components onto an existing EKS cluster baseline.
# region, vpc_id, scope_ids, and cluster_name are inherited from the cluster.
resource "duploai_cluster_attributes" "basic" {
  workspace_id = "<workspace-id>"
  name         = "<cluster-attributes-name>"
  cluster_id   = "<cluster-id>"

  components = {
    cluster_autoscaler           = true
    alb_load_balancer_controller = true
    efs_volumes                  = true
    metrics_server               = true
    external_dns                 = false
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

# Full example linked to a cluster baseline resource in the same configuration,
# with a custom provisioner and extended timeouts.
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
    alb_load_balancer_controller = true
    efs_volumes                  = true
    metrics_server               = true
    kube_state_metrics           = true
    flux_cd                      = false
    external_dns                 = true
  }

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
