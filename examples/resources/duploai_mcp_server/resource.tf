# HTTP MCP server — connects to a remote server over HTTP.
resource "duploai_mcp_server" "http" {
  name          = "github-tools"
  config_type   = "Http"
  api_endpoint  = "https://mcp.example.com/github"
  transport     = "http"
  provider_type = "other"
}

# SSE MCP server — streaming transport.
resource "duploai_mcp_server" "sse" {
  name         = "search-tools"
  config_type  = "Sse"
  api_endpoint = "https://mcp.example.com/search/sse"
  transport    = "sse"
}

# Raw MCP server — flat key/value configuration.
resource "duploai_mcp_server" "raw" {
  name        = "local-tools"
  config_type = "Raw"

  raw_config = {
    command = "uvx"
    server  = "mcp-server-time"
  }
}
