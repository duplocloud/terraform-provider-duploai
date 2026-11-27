# Read-only access to one workspace, plus the scopes inside it.
#
# Every rule path starts at a Workspace segment. allow_all_children makes the
# grant cover the whole subtree beneath the workspace, including resources
# created after the grant is written.
resource "duploai_admin_permission_set" "read_only" {
  name        = "read-only"
  description = "GET on everything in one workspace"

  resource_grants = {
    allow = [
      {
        path = [
          { node_type = "Workspace", node_id = "<workspace-id>" }
        ]
        verbs              = ["GET"]
        scopes             = ["*"]
        allow_all_children = true
      }
    ]
  }
}

# Full access to a workspace, with two exceptions.
#
# ALL grants the four CRUD verbs and NOTHING else: named actions are never
# implied by a verb, so every action the holder should keep has to be listed --
# "*" for all of them, or the individual action ids.
resource "duploai_admin_permission_set" "devops" {
  name        = "devops-access"
  description = "Workspace owner, minus production databases and secret writes"

  resource_grants = {
    allow = [
      {
        path = [
          { node_type = "Workspace", node_id = "<workspace-id>" }
        ]
        verbs              = ["ALL"]
        actions            = ["*"]
        scopes             = ["*"]
        allow_all_children = true
      }
    ]

    deny = [
      # A deny always covers its target's whole subtree, so this cuts the
      # production resource group entirely -- allow_all_children is not needed
      # (and is ignored) on a deny rule.
      {
        path = [
          { node_type = "Workspace", node_id = "<workspace-id>" },
          { node_type = "Environment", node_id = "<environment-id>" },
          { node_type = "ResourceGroup", node_id = "<prod-resource-group-id>" },
        ]
        verbs = ["ALL"]
      },

      # Verb-scoped: reading secrets stays allowed, writing them does not. A
      # non-empty actions list blocks only the actions named.
      {
        path = [
          { node_type = "Workspace", node_id = "<workspace-id>" }
        ]
        actions = ["secret.write"]
      },
    ]
  }
}

# Narrow grants that name individual resources and a create-only path.
#
# The path is the containment chain root to leaf: workspace, environment,
# resource group, then the category segment a cloud resource sits under ("aws",
# "k8s", "terraform"), then the resource itself.
resource "duploai_admin_permission_set" "sqs_operator" {
  name        = "sqs-operator"
  description = "Manage SQS queues in one resource group, nothing else"

  resource_grants = {
    allow = [
      # A wildcard node_id is only ever the LAST segment, and names the type as
      # a whole rather than one object. POST here is what permits creating new
      # queues in this resource group.
      {
        path = [
          { node_type = "Workspace", node_id = "<workspace-id>" },
          { node_type = "Environment", node_id = "<environment-id>" },
          { node_type = "ResourceGroup", node_id = "<resource-group-id>" },
          { node_type = "AWS", node_id = "aws" },
          { node_type = "AwsSqsQueue", node_id = "*" },
        ]
        verbs              = ["GET", "POST", "PUT", "DELETE"]
        allow_all_children = true
      },

      # Scopes are read only from a rule whose path ends at the Workspace, so
      # the scope grant is its own rule.
      {
        path = [
          { node_type = "Workspace", node_id = "<workspace-id>" }
        ]
        verbs  = ["GET"]
        scopes = ["<scope-id>"]
      },
    ]
  }
}

# Deprecated shape, kept for configurations written before resource grants
# existed. It no longer grants access on its own and cannot be combined with
# resource_grants in the same configuration.
resource "duploai_admin_permission_set" "legacy" {
  name = "legacy-read-only"

  allowed_workspaces = [
    {
      workspace_id   = "<workspace-id>"
      allowed_scopes = ["prod-.*"]
      denied_scopes  = ["prod-payments"]
    }
  ]
}
