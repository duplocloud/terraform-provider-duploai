output "network_id" { value = duploai_network_baseline.this.network_id }
output "cluster_id" { value = split("/", duploai_cluster_baseline.this.id)[1] }
