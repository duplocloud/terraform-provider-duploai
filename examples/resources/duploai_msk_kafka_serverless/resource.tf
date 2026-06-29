# A serverless MSK (Kafka) cluster — AWS manages broker capacity automatically.
# Networking and IAM client authentication are configured server-side.
resource "duploai_msk_kafka_serverless" "example" {
  workspace_id      = "6a25105705686d697e0da225"
  name              = "my-serverless-kafka"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"
}

output "kafka_bootstrap_brokers_sasl_iam" {
  value = duploai_msk_kafka_serverless.example.bootstrap_brokers_sasl_iam
}
