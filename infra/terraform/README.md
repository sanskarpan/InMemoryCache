# Terraform Deployment

This module provisions the baseline Kubernetes resources for Cache Engine:

- namespace
- config map
- secret
- persistent volume claim
- backend deployment and service
- frontend deployment and service
- ingress

Example:

```bash
terraform init
terraform apply \
  -var='host=cache.example.com' \
  -var='allowed_origins=https://cache.example.com' \
  -var='api_key=replace-me'
```

For production deployments, set `api_image` and `web_image` to digest-pinned release references instead of mutable tags.

The Terraform module and the raw manifests under `deploy/kubernetes/` intentionally model the same deployment shape. Use the raw manifests when you need direct YAML control, and Terraform when you want environment provisioning and drift management.

The Prometheus `ServiceMonitor`, scrape-auth secret example, and alert rules are kept as raw manifests under `deploy/monitoring/` because they depend on the Prometheus Operator CRDs being installed in the target cluster.
