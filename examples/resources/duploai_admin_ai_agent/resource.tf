# Basic AWS Bedrock agent — minimal required fields.
resource "duploai_admin_ai_agent" "bedrock_basic" {
  name     = "bedrock-claude"
  endpoint = "https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude-3-5-sonnet-20240620-v1:0/invoke"
}

# Full AWS Bedrock agent — streaming, endpoint details, and free-form metadata.
resource "duploai_admin_ai_agent" "bedrock_full" {
  name                   = "bedrock-claude-streaming"
  endpoint               = "https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude-3-5-sonnet-20240620-v1:0/invoke-with-response-stream"
  description            = "AWS Bedrock Claude agent for ticket triage"
  does_support_streaming = true

  endpoint_details = {
    path   = "/model/anthropic.claude-3-5-sonnet-20240620-v1:0/invoke-with-response-stream"
    method = "POST"
    http_headers = {
      "Content-Type" = "application/json"
      "Accept"       = "application/vnd.amazon.eventstream"
    }
  }

  metadata = {
    model      = "anthropic.claude-3-5-sonnet-20240620-v1:0"
    region     = "us-east-1"
    max_tokens = "4096"
  }
}
