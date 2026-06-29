# Look up a serverless MSK cluster by workspace and ID.
data "duploai_msk_kafka_serverless" "example" {
  workspace_id = "<workspace-id>"
  id           = "<msk-id>"
}

output "bootstrap_brokers_sasl_iam" {
  value = data.duploai_msk_kafka_serverless.example.bootstrap_brokers_sasl_iam
}

output "status" {
  value = data.duploai_msk_kafka_serverless.example.status
}
