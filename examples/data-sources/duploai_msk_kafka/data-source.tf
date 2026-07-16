# Look up a provisioned MSK cluster by workspace and ID.
data "duploai_msk_kafka" "example" {
  workspace_id = "<workspace-id>"
  id           = "<msk-id>"
}

output "bootstrap_brokers_tls" {
  value = data.duploai_msk_kafka.example.bootstrap_brokers_tls
}

output "status" {
  value = data.duploai_msk_kafka.example.status
}
