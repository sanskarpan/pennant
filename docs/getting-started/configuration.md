# Configuration

Pennant is configured entirely through environment variables. A `config.yaml` file provides advanced tuning knobs that rarely need changing.

## Environment variables

### Required

| Variable | Description |
|---|---|
| `PENNANT_JWT_SECRET` | HS256 signing key for JWT tokens. **Minimum 32 characters.** Required in production — the server refuses to start without it if no legacy token is set. |

### Storage

Exactly one storage backend is used. Selection priority: `DATABASE_URL` > `SQLITE_PATH` > in-memory.

| Variable | Description |
|---|---|
| `DATABASE_URL` | PostgreSQL connection string, e.g. `postgres://user:pass@host:5432/dbname?sslmode=disable`. Enables multi-node operation. |
| `SQLITE_PATH` | Absolute path to the SQLite database file, e.g. `/data/pennant.db`. Good for single-node production deployments. |

!!! info "In-memory mode"
    If neither `DATABASE_URL` nor `SQLITE_PATH` is set, Pennant uses an in-memory store. All data is lost on restart. Useful for development and tests — see `make run`.

### Authentication and security

| Variable | Description |
|---|---|
| `PENNANT_ADMIN_TOKEN` | Legacy static bearer token (pre-JWT). Leave empty when using JWT. If set, requests with `Authorization: Bearer <token>` bypass JWT validation and receive `owner` role. |
| `PENNANT_CORS_ORIGINS` | Comma-separated list of allowed CORS origins, e.g. `https://app.example.com,https://admin.example.com`. Empty string allows all origins — acceptable in development, **not** in production. |

### TLS

TLS mode is selected by priority: `TLS_AUTO_DOMAIN` > (`TLS_CERT_FILE` + `TLS_KEY_FILE`) > plain HTTP.

| Variable | Description |
|---|---|
| `TLS_AUTO_DOMAIN` | Domain name for automatic Let's Encrypt certificate, e.g. `flags.example.com`. The server listens on `:443` and handles ACME HTTP-01 challenges automatically. Requires the domain to point at the server's public IP and port 80 to be reachable. |
| `TLS_CERT_FILE` | Absolute path to a PEM-encoded TLS certificate. Must be set together with `TLS_KEY_FILE`. |
| `TLS_KEY_FILE` | Absolute path to the corresponding PEM-encoded private key. |

## config.yaml reference

The `config.yaml` file at the repo root contains advanced settings. You can mount a custom one into the container or modify it before building.

```yaml
stream:
  heartbeat_interval: 25s     # How often to send SSE keepalive events
  replay_ring_size: 256        # Number of past events stored for delta replay on reconnect
  slow_client_policy: disconnect  # What to do with clients that can't keep up: disconnect | drop

snapshot:
  delta_threshold: 0.30        # Send a delta patch if the diff is < 30% of the full snapshot size;
                               # otherwise send a full snapshot

evaluation:
  bucket_scale: 1152921504606846975  # 0xFFFFFFFFFFFFFFF (2^60 - 1). Do not change.

analytics:
  ingest_workers: 4            # Number of goroutines draining the analytics event queue
  flush_interval: 1s           # How often workers flush batched events to the database

experiments:
  default_alpha: 0.05          # Default significance threshold for mSPRT / z-test
  srm_threshold: 0.001         # p-value below which a Sample Ratio Mismatch is flagged
```

!!! warning "bucket_scale"
    `evaluation.bucket_scale` is the denominator used in every bucketing calculation. Changing it will alter which bucket every user lands in and invalidate all existing percentage rollouts and experiment assignments. Never change it on a running system.

## Example .env file

```bash
# Required
PENNANT_JWT_SECRET=a-very-long-random-secret-that-is-at-least-32-chars

# Storage (choose one)
# SQLITE_PATH=/data/pennant.db
# DATABASE_URL=postgres://pennant:pennant@db:5432/pennant?sslmode=disable

# Security
PENNANT_CORS_ORIGINS=https://app.example.com

# TLS (optional — choose one mode)
# TLS_AUTO_DOMAIN=flags.example.com
# TLS_CERT_FILE=/certs/tls.crt
# TLS_KEY_FILE=/certs/tls.key
```

## Rate limiting defaults

These are hardcoded in `internal/ratelimit/` and not yet configurable via environment variables:

| Endpoint | Limit |
|---|---|
| `POST /auth/login` | 5 requests / minute / IP |
| SDK key endpoints | Per-key bucket (prevents runaway clients) |

## Default seed data

On every first start (when the users table is empty) Pennant inserts:

- User `admin@pennant.local` with password `admin`, role `owner`
- Project `default` with environment `production`
- SDK server key `sdk-server-default-prod`
- SDK client key `sdk-client-default-prod`

In production, create a separate admin account with a strong password and delete or disable the seed account.
