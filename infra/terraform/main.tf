locals {
  labels = {
    app = "cache-engine"
  }
}

resource "kubernetes_namespace" "cache_engine" {
  metadata {
    name = var.namespace
  }
}

resource "kubernetes_config_map_v1" "cache_engine" {
  metadata {
    name      = "cache-engine-config"
    namespace = kubernetes_namespace.cache_engine.metadata[0].name
  }

  data = {
    CACHE_ENGINE_ENV                  = "production"
    CACHE_ENGINE_ADDR                 = ":8080"
    CACHE_ENGINE_ALLOWED_ORIGINS      = var.allowed_origins
    CACHE_ENGINE_STATE_DB_PATH        = "/app/data/cache-engine.db"
    CACHE_ENGINE_BACKING_STORE_DRIVER = "sqlite"
    CACHE_ENGINE_RATE_LIMIT_REQUESTS  = "120"
    CACHE_ENGINE_RATE_LIMIT_WINDOW_MS = "60000"
    CACHE_ENGINE_SSE_TOKEN_TTL_MS     = "120000"
    CACHE_ENGINE_SHUTDOWN_TIMEOUT_MS  = "15000"
    CACHE_ENGINE_LOG_FORMAT           = "json"
    CACHE_ENGINE_LOG_LEVEL            = "info"
    CACHE_ENGINE_SEED_DEMO_DATA       = "false"
  }
}

resource "kubernetes_secret_v1" "cache_engine" {
  metadata {
    name      = "cache-engine-secrets"
    namespace = kubernetes_namespace.cache_engine.metadata[0].name
  }

  data = {
    CACHE_ENGINE_API_KEY = var.api_key
  }
}

resource "kubernetes_persistent_volume_claim_v1" "state" {
  wait_until_bound = false

  metadata {
    name      = "cache-engine-state"
    namespace = kubernetes_namespace.cache_engine.metadata[0].name
  }

  spec {
    access_modes = ["ReadWriteOnce"]
    resources {
      requests = {
        storage = var.state_db_size
      }
    }
  }
}

resource "kubernetes_deployment_v1" "api" {
  metadata {
    name      = "cache-engine-api"
    namespace = kubernetes_namespace.cache_engine.metadata[0].name
    labels    = merge(local.labels, { component = "api" })
  }

  spec {
    replicas = 1

    selector {
      match_labels = {
        app       = "cache-engine"
        component = "api"
      }
    }

    template {
      metadata {
        labels = {
          app       = "cache-engine"
          component = "api"
        }
      }

      spec {
        security_context {
          fs_group = 65532
        }

        container {
          name              = "api"
          image             = var.api_image
          image_pull_policy = "IfNotPresent"

          security_context {
            run_as_non_root = true
            run_as_user     = 65532
          }

          port {
            name           = "http"
            container_port = 8080
          }

          env_from {
            config_map_ref {
              name = kubernetes_config_map_v1.cache_engine.metadata[0].name
            }
          }

          env_from {
            secret_ref {
              name = kubernetes_secret_v1.cache_engine.metadata[0].name
            }
          }

          volume_mount {
            name       = "state"
            mount_path = "/app/data"
          }

          liveness_probe {
            http_get {
              path = "/healthz"
              port = "http"
            }
            initial_delay_seconds = 10
            period_seconds        = 20
          }

          readiness_probe {
            http_get {
              path = "/readyz"
              port = "http"
            }
            initial_delay_seconds = 5
            period_seconds        = 10
          }
        }

        volume {
          name = "state"
          persistent_volume_claim {
            claim_name = kubernetes_persistent_volume_claim_v1.state.metadata[0].name
          }
        }
      }
    }
  }
}

resource "kubernetes_service_v1" "api" {
  metadata {
    name      = "cache-engine-api"
    namespace = kubernetes_namespace.cache_engine.metadata[0].name
  }

  spec {
    selector = {
      app       = "cache-engine"
      component = "api"
    }

    port {
      name        = "http"
      port        = 8080
      target_port = "http"
    }
  }
}

resource "kubernetes_deployment_v1" "web" {
  metadata {
    name      = "cache-engine-web"
    namespace = kubernetes_namespace.cache_engine.metadata[0].name
    labels    = merge(local.labels, { component = "web" })
  }

  spec {
    replicas = 1

    selector {
      match_labels = {
        app       = "cache-engine"
        component = "web"
      }
    }

    template {
      metadata {
        labels = {
          app       = "cache-engine"
          component = "web"
        }
      }

      spec {
        container {
          name              = "web"
          image             = var.web_image
          image_pull_policy = "IfNotPresent"

          env {
            name  = "CACHE_ENGINE_UPSTREAM_HOST"
            value = "cache-engine-api.${kubernetes_namespace.cache_engine.metadata[0].name}.svc.cluster.local"
          }

          env {
            name = "CACHE_ENGINE_UPSTREAM_API_KEY"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.cache_engine.metadata[0].name
                key  = "CACHE_ENGINE_API_KEY"
              }
            }
          }

          port {
            name           = "http"
            container_port = 80
          }
        }
      }
    }
  }
}

resource "kubernetes_service_v1" "web" {
  metadata {
    name      = "cache-engine-web"
    namespace = kubernetes_namespace.cache_engine.metadata[0].name
  }

  spec {
    selector = {
      app       = "cache-engine"
      component = "web"
    }

    port {
      name        = "http"
      port        = 80
      target_port = "http"
    }
  }
}

resource "kubernetes_ingress_v1" "cache_engine" {
  metadata {
    name      = "cache-engine"
    namespace = kubernetes_namespace.cache_engine.metadata[0].name
  }

  spec {
    ingress_class_name = "nginx"

    rule {
      host = var.host

      http {
        path {
          path      = "/api"
          path_type = "Prefix"
          backend {
            service {
              name = kubernetes_service_v1.api.metadata[0].name
              port {
                number = 8080
              }
            }
          }
        }

        path {
          path      = "/metrics"
          path_type = "Prefix"
          backend {
            service {
              name = kubernetes_service_v1.api.metadata[0].name
              port {
                number = 8080
              }
            }
          }
        }

        path {
          path      = "/healthz"
          path_type = "Prefix"
          backend {
            service {
              name = kubernetes_service_v1.api.metadata[0].name
              port {
                number = 8080
              }
            }
          }
        }

        path {
          path      = "/readyz"
          path_type = "Prefix"
          backend {
            service {
              name = kubernetes_service_v1.api.metadata[0].name
              port {
                number = 8080
              }
            }
          }
        }

        path {
          path      = "/"
          path_type = "Prefix"
          backend {
            service {
              name = kubernetes_service_v1.web.metadata[0].name
              port {
                number = 80
              }
            }
          }
        }
      }
    }
  }
}
