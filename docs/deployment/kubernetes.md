# Kubernetes Deployment

Pennant runs well on Kubernetes. The server exposes standard HTTP health probes and emits Prometheus metrics. Here is a complete single-replica deployment with a PostgreSQL backend.

## Namespace and secret

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: pennant
---
apiVersion: v1
kind: Secret
metadata:
  name: pennant-secrets
  namespace: pennant
type: Opaque
stringData:
  jwt-secret: "your-long-random-secret-at-least-32-chars"
  database-url: "postgres://pennant:password@postgres-svc:5432/pennant?sslmode=disable"
```

!!! warning "Do not commit secrets"
    Use a secrets management solution (External Secrets Operator, Sealed Secrets, AWS Secrets Manager, HashiCorp Vault) rather than storing plaintext secrets in manifests.

## Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pennant
  namespace: pennant
  labels:
    app: pennant
spec:
  replicas: 2
  selector:
    matchLabels:
      app: pennant
  template:
    metadata:
      labels:
        app: pennant
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
        prometheus.io/path: "/metrics"
    spec:
      terminationGracePeriodSeconds: 30
      containers:
        - name: pennant
          image: ghcr.io/sanskarpan/pennant:v1.0.0
          ports:
            - containerPort: 8080
              name: http
          env:
            - name: PENNANT_JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: pennant-secrets
                  key: jwt-secret
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: pennant-secrets
                  key: database-url
            - name: PENNANT_CORS_ORIGINS
              value: "https://admin.yourapp.com"
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: /ready
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 5
            failureThreshold: 3
          resources:
            requests:
              cpu: "100m"
              memory: "64Mi"
            limits:
              cpu: "500m"
              memory: "256Mi"
          securityContext:
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 1000
            allowPrivilegeEscalation: false
```

### Liveness vs readiness

| Probe | Endpoint | Purpose |
|---|---|---|
| Liveness | `GET /health` | Restarts the pod if the store connection is lost and not recovering |
| Readiness | `GET /ready` | Removes the pod from service endpoints during startup or store outage |

Both return `200 OK` when healthy and `503 Service Unavailable` when not. The difference is in Kubernetes behavior: a failed liveness probe triggers a pod restart; a failed readiness probe removes the pod from the Service's endpoint slice without restarting it.

## Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: pennant-svc
  namespace: pennant
spec:
  selector:
    app: pennant
  ports:
    - port: 80
      targetPort: 8080
      name: http
  type: ClusterIP
```

## Ingress (with TLS)

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: pennant-ingress
  namespace: pennant
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - flags.yourapp.com
      secretName: pennant-tls
  rules:
    - host: flags.yourapp.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: pennant-svc
                port:
                  name: http
```

!!! important "SSE timeout configuration"
    SSE connections are long-lived HTTP connections. The `proxy-read-timeout` and `proxy-send-timeout` annotations increase NGINX's default 60-second timeout to 1 hour. Without this, NGINX will close SSE connections every 60 seconds, triggering constant client reconnects. Other ingress controllers (Traefik, Envoy/Istio) have equivalent settings.

## Multi-replica notes

Multiple Pennant replicas are safe when using PostgreSQL. Each replica:
- Maintains its own in-memory SSE hub
- Persists flag changes to the shared PostgreSQL database
- Reads changes from the database on each write

**Important:** A flag update made to replica A is persisted to Postgres and immediately streamed to SDK clients connected to replica A. SDK clients connected to replica B will receive the update on their next SSE heartbeat or reconnect. The maximum propagation delay is one heartbeat interval (default 25 seconds).

For zero-lag cross-replica propagation, you could add a Postgres `LISTEN/NOTIFY` channel — this is not currently built-in but is a natural extension point in `internal/store/postgres.go`.

## PodDisruptionBudget

Keep at least one replica available during rolling updates:

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: pennant-pdb
  namespace: pennant
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: pennant
```

## Horizontal Pod Autoscaler

Pennant scales horizontally (with PostgreSQL). Scale on CPU or RPS:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: pennant-hpa
  namespace: pennant
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: pennant
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
```

## Resource sizing guidelines

| Scale | Replicas | CPU request | Memory request |
|---|---|---|---|
| < 100 flags, < 1K SDK clients | 1 | 50m | 32Mi |
| < 1K flags, < 10K SDK clients | 2 | 100m | 64Mi |
| < 10K flags, < 100K SDK clients | 3–5 | 250m | 128Mi |
| > 10K flags or > 100K SDK clients | Profile first | 500m+ | 256Mi+ |

These are starting points. The SSE hub's memory usage scales with the number of concurrent connected clients. Profile with your actual load using `make loadgen`.
