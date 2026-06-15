output "frontend_status" { value = duploai_app_service.frontend.status }
output "backend_status" { value = duploai_app_service.backend.status }
output "db_status" { value = duploai_rds_instance.db.status }
