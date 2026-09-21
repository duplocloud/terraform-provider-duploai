# A provisioned MSK (Kafka) cluster sized by you.
resource "duploai_msk_kafka" "example" {
  workspace_id      = "6a25105705686d697e0da225"
  name              = "my-kafka-cluster"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  instance_type          = "kafka.m5.large"
  number_of_broker_nodes = 3
  broker_volume_size_gb  = 100
  kafka_version          = "3.6.0"

  # Optional: pin specific private subnets (one broker per AZ). Omit for all RG private subnets.
  subnet_ids = ["subnet-0a727a1199534ecf3", "subnet-0fc56ead225db9bcc"]

  # ResourceGroupKmsKey + kms_key_id encrypts data at rest with a registered
  # customer-managed key.
  encryption            = "AwsManagedKey"
  encryption_in_transit = "TLS"
}

output "kafka_bootstrap_brokers_tls" {
  value = duploai_msk_kafka.example.bootstrap_brokers_tls
}

output "kafka_cluster_arn" {
  value = duploai_msk_kafka.example.cluster_arn
}
