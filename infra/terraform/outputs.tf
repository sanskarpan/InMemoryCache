output "namespace" {
  value = kubernetes_namespace.cache_engine.metadata[0].name
}

output "backend_service" {
  value = kubernetes_service_v1.api.metadata[0].name
}

output "frontend_service" {
  value = kubernetes_service_v1.web.metadata[0].name
}

output "ingress_host" {
  value = var.host
}
