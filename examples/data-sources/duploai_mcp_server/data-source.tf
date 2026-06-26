# Look up an MCP server by ID.
data "duploai_mcp_server" "example" {
  id = "<object-id>"
}

output "name" {
  value = data.duploai_mcp_server.example.name
}

output "config_type" {
  value = data.duploai_mcp_server.example.config_type
}

output "api_endpoint" {
  value = data.duploai_mcp_server.example.api_endpoint
}
