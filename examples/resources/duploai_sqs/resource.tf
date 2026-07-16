# A standard SQS queue with KMS encryption and a long-poll receive.
resource "duploai_sqs" "orders" {
  workspace_id      = "<workspace-id>"
  name              = "orders"
  queue_type        = "Standard"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  visibility_timeout_seconds   = 60
  message_retention_seconds    = 345600
  receive_message_wait_seconds = 10

  encryption_mode   = "SseKms"
  kms_master_key_id = "alias/aws/sqs"

  tags = {
    team = "platform"
    app  = "orders"
  }

  timeouts {
    create = "30m"
    delete = "15m"
  }
}

# A FIFO queue (the platform appends the ".fifo" suffix to the queue name).
resource "duploai_sqs" "events" {
  workspace_id      = "<workspace-id>"
  name              = "events"
  queue_type        = "Fifo"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  content_based_deduplication = true

  timeouts {
    create = "30m"
    delete = "15m"
  }
}

output "orders_queue_url" {
  value = duploai_sqs.orders.queue_url
}
