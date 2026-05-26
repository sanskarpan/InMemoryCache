# Kubernetes manifests

Base production-shaped manifests live in this directory and can be applied with:

```bash
kubectl apply -k deploy/kubernetes
```

Before applying the base manifests:

- replace the placeholder API key in `secret.example.yaml`
- set the real ingress host in `ingress.yaml`
- point `CACHE_ENGINE_ALLOWED_ORIGINS` in `configmap.yaml` at the deployed frontend origin

## Local kind validation

A disposable local overlay is included under `overlays/kind` for end-to-end validation against a local Kubernetes cluster. It:

- rewrites the container images to `cache-engine-api:kind` and `cache-engine-web:kind`
- sets browser origins to `http://127.0.0.1:8081,http://localhost:8081`
- injects a local-only API key of `kind-local-key`
- rewrites the ingress host to `cache-engine.localtest.me`

Typical flow:

```bash
kind create cluster --name cache-engine
docker build -t cache-engine-api:kind ./cache-engine
docker build -t cache-engine-web:kind ./cache-engine/web
kind load docker-image cache-engine-api:kind --name cache-engine
kind load docker-image cache-engine-web:kind --name cache-engine
kubectl kustomize --load-restrictor=LoadRestrictionsNone deploy/kubernetes/overlays/kind | kubectl apply -f -
kubectl -n cache-engine port-forward svc/cache-engine-web 8081:80
kubectl -n cache-engine port-forward svc/cache-engine-api 18080:8080
```

Then validate:

- `http://127.0.0.1:8081/` for the frontend
- `http://127.0.0.1:8081/api/cache/lru/stats` through the frontend proxy
- `http://127.0.0.1:18080/healthz` directly against the backend

The Prometheus `ServiceMonitor`, scrape-auth secret example, and alert rules are kept in `deploy/monitoring/` because they require Prometheus Operator CRDs in the target cluster.
