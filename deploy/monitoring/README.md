# Monitoring manifests

These manifests target a Prometheus Operator installation and assume the operator itself runs in the `monitoring` namespace.

Apply with:

```bash
kubectl apply -k deploy/monitoring
```

Before applying:

- replace the placeholder API key in `secret.example.yaml`
- ensure the API key matches the one configured for the Cache Engine backend

The `ServiceMonitor` lives in `monitoring` and scrapes the `cache-engine-api` service in the `cache-engine` namespace. The dedicated monitoring secret keeps scrape credentials in the namespace where the operator and Prometheus instance typically run.
