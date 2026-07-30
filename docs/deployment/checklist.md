# Production Checklist

Work through this checklist before exposing Pennant on a public network or using it to gate production traffic.

## Security

- [ ] **Set `PENNANT_JWT_SECRET`** to a randomly generated string of at least 32 characters. Do not use the default `change-me-in-production`.

    ```bash
    openssl rand -base64 48
    ```

- [ ] **Change the default admin password.** The seeded `admin@pennant.local` / `admin` account is public knowledge. Log in and update the password immediately, or create a new `owner` account and delete the seed account.

- [ ] **Restrict `PENNANT_CORS_ORIGINS`.** Set it to a comma-separated list of your dashboard's allowed origins. Empty string allows all origins — not suitable for production.

    ```bash
    PENNANT_CORS_ORIGINS=https://admin.yourapp.com
    ```

- [ ] **Enable TLS.** Either set `TLS_AUTO_DOMAIN` for automatic Let's Encrypt certificates, or provide `TLS_CERT_FILE` + `TLS_KEY_FILE` for an existing certificate. Do not run plain HTTP in production.

- [ ] **Remove `PENNANT_ADMIN_TOKEN`** if it was set for bootstrapping. Prefer JWT-based auth and service-account users.

- [ ] **Rotate SDK keys.** The default SDK keys (`sdk-server-default-prod`, `sdk-client-default-prod`) are seeded with known values. Create fresh keys in the dashboard and update your applications before going live.

- [ ] **Scope SDK keys.** Create separate SDK keys for each consuming application. This allows you to revoke a single key without affecting other services.

## Storage

- [ ] **Choose a persistent store.** Do not use the in-memory store in production. Use `SQLITE_PATH` for single-node deployments or `DATABASE_URL` for multi-node.

- [ ] **Back up regularly.** If using SQLite, snapshot the database file to an external location (S3, GCS, etc.) at least daily. If using PostgreSQL, enable `pg_dump` or a managed backup service.

- [ ] **Test restore.** Verify your backup can be restored before you need it.

- [ ] **For PostgreSQL:** Use a connection pool (Pennant uses `pgxpool` internally with default pool settings). Ensure your database allows at least `max_pool_size` connections from Pennant.

## Performance

- [ ] **Tune `analytics.ingest_workers`** if you expect high event volume (> 10K events/sec). Default is 4 workers.

- [ ] **Review `stream.replay_ring_size`** if clients frequently reconnect after long gaps. Default is 256 events. Increase if you have many flags and frequent updates.

- [ ] **Set resource limits** in your container/Kubernetes spec. Pennant is lightweight (typically < 50 MB RAM for SQLite deployments with < 1000 flags).

## Observability

- [ ] **Scrape `/metrics`.** Connect Prometheus to the `/metrics` endpoint and set up alerts for:
    - High HTTP error rates (`pennant_http_requests_total` with `status=~"5.."`)
    - SSE connection drops
    - Event queue saturation

- [ ] **Configure `/health` and `/ready` probes** in your load balancer and Kubernetes deployment. Use `/ready` as the readiness probe and `/health` as the liveness probe.

- [ ] **Set up log aggregation.** Pennant emits structured JSON logs to stdout. Ship them to your preferred log aggregation system (Datadog, Splunk, CloudWatch, etc.).

- [ ] **Alert on SRM detection.** If you use A/B testing, monitor experiment results for `srmDetected: true` and alert your data team.

## Deployment hygiene

- [ ] **Pin the Docker image tag.** Don't use `latest` in production. Pin to a specific version tag.

- [ ] **Use secrets management.** Don't hardcode `PENNANT_JWT_SECRET` or database credentials in `docker-compose.yml` or Kubernetes manifests. Use Docker secrets, Kubernetes Secrets, AWS Secrets Manager, HashiCorp Vault, etc.

- [ ] **Health check before routing traffic.** Use the readiness probe to ensure the server has initialized before sending production traffic.

- [ ] **Graceful shutdown.** Pennant handles `SIGTERM` by stopping new connections, waiting for in-flight requests to complete, and flushing pending analytics events. Ensure your orchestrator sends `SIGTERM` and waits for the process to exit (Kubernetes `terminationGracePeriodSeconds` default of 30s is sufficient).

- [ ] **Test failover.** Kill the Pennant server and verify that your applications continue to operate with the last known flag values (SDK in-memory snapshot). Bring the server back up and verify that SDK clients reconnect and receive updated flags.

## Checklist summary

| Category | Items | Critical |
|---|---|---|
| Security | JWT secret, password, CORS, TLS, SDK keys | All |
| Storage | Persistent store, backups | `SQLITE_PATH` or `DATABASE_URL` |
| Observability | Metrics, health probes, logs | `/health`, `/ready` |
| Deployment | Secrets management, graceful shutdown | Secrets |
