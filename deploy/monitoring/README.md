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

## Dashboard

The bundle also includes a Grafana dashboard ConfigMap named `cache-engine-dashboard`. It is labeled for the standard Grafana sidecar convention used by `kube-prometheus-stack`. Import it automatically through the sidecar or manually in Grafana using the JSON payload in `cache-engine-dashboard.json`.

Panels cover:

- HTTP request rate
- HTTP error rate
- auth and rate-limit rejection rates
- readiness and benchmark activity
- process memory
- store hit ratio

## Alert routing

`alertmanager-config.example.yaml` shows a practical `AlertmanagerConfig` route tree for Prometheus Operator deployments. It is intentionally left as an example file because the receiver URLs and secret names are cluster-specific.

To use it:

1. create the referenced webhook secrets in the `monitoring` namespace
2. replace the example webhook URLs with your real routing endpoints
3. apply the resource alongside the rest of the monitoring bundle

The alert rules in `prometheus-rules.yaml` continue to focus on detection; the AlertmanagerConfig is what forwards critical and warning events to the right destinations.
