# Standard SNS topic.
resource "duploai_sns" "standard" {
  workspace_id      = "<workspace-id>"
  name              = "orders-events"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  display_name = "Order Events"
}

# FIFO SNS topic with content-based deduplication and KMS encryption.
# The ".fifo" suffix is appended automatically for FIFO topics.
resource "duploai_sns" "fifo" {
  workspace_id      = "<workspace-id>"
  name              = "orders-events-ordered"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  topic_type                  = "Fifo"
  content_based_deduplication = true

  encryption_mode   = "SseKms"
  kms_master_key_id = "alias/aws/sns"

  tags = [
    { key = "team", value = "payments" }
  ]
}
