# Look up a node group by ID.
data "duploai_node_group" "example" {
  workspace_id = "<workspace-id>"
  id           = "<object-id>"
}

output "status" {
  value = data.duploai_node_group.example.status
}
