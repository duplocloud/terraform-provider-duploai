# Import an existing Azure PostgreSQL Flexible Server.
#  - WORKSPACE_ID is the ID of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - SERVER_ID is the ID of the server record (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_azure_postgres_flexible_server.example WORKSPACE_ID/SERVER_ID
# Example:
# terraform import duploai_azure_postgres_flexible_server.example 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e
#
# administrator_password is write-only — the API never returns it, so it lands
# empty after an import. Set it in config and apply to bring the two in step.
