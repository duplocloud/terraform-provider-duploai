output "frontend_status" { value = duploai_app_service.frontend.status }
output "backend_status" { value = duploai_app_service.backend.status }
output "db_status" { value = duploai_rds_instance.db.status }
output "ingress_status" { value = duploai_k8s_ingress.demo.status }
