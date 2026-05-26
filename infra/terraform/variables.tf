variable "kubeconfig_path" {
  type        = string
  description = "Path to the kubeconfig file."
  default     = "~/.kube/config"
}

variable "kube_context" {
  type        = string
  description = "Kubernetes context to use."
  default     = null
}

variable "namespace" {
  type        = string
  description = "Deployment namespace."
  default     = "cache-engine"
}

variable "host" {
  type        = string
  description = "Ingress host for the application."
}

variable "api_key" {
  type        = string
  description = "Production API key for the cache engine."
  sensitive   = true
}

variable "api_image" {
  type        = string
  description = "Backend container image."
  default     = "ghcr.io/your-org/cache-engine-api:latest"
}

variable "web_image" {
  type        = string
  description = "Frontend container image."
  default     = "ghcr.io/your-org/cache-engine-web:latest"
}

variable "allowed_origins" {
  type        = string
  description = "Comma-separated browser origins."
}

variable "state_db_size" {
  type        = string
  description = "PVC size for SQLite state."
  default     = "5Gi"
}
