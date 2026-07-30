# Docker Compose

Pennant ships with a `docker-compose.yml` that gives you a working stack in one command.

## Default stack (SQLite)

The default configuration uses SQLite — no external database required. All flag and user data is stored in a named Docker volume.

```yaml
services:
  pennant:
    build: .
    ports:
      - "8080:8080"
    environment:
      PENNANT_JWT_SECRET: change-me-in-production
      SQLITE_PATH: /data/pennant.db
      # DATABASE_URL: postgres://pennant:pennant@db:5432/pennant?sslmode=disable
    volumes:
      - pennant-data:/data
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 3
    restart: unless-stopped

volumes:
  pennant-data:
```

Start it:

```bash
docker compose up -d
docker compose logs -f pennant
```

## PostgreSQL stack

For multi-node or higher-availability setups, switch the storage backend to PostgreSQL. Uncomment `DATABASE_URL` and add the `db` service:

```yaml
services:
  pennant:
    build: .
    ports:
      - "8080:8080"
    environment:
      PENNANT_JWT_SECRET: change-me-in-production
      DATABASE_URL: postgres://pennant:pennant@db:5432/pennant?sslmode=disable
    depends_on:
      db:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 5
    restart: unless-stopped

  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: pennant
      POSTGRES_USER: pennant
      POSTGRES_PASSWORD: pennant
    volumes:
      - pg-data:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U pennant"]
      interval: 5s
      timeout: 3s
      retries: 10
    restart: unless-stopped

volumes:
  pg-data:
```

!!! tip "Waiting for the database"
    The `depends_on: condition: service_healthy` ensures Pennant only starts after Postgres has passed its `pg_isready` health check. Without this, Pennant may fail to start if Postgres is still initializing.

## Environment variable override

Never commit secrets to `docker-compose.yml`. Instead, use a `.env` file in the same directory:

```bash
# .env
PENNANT_JWT_SECRET=a-very-long-random-secret-that-is-at-least-32-chars
POSTGRES_PASSWORD=strong-password-here
```

Reference them in `docker-compose.yml`:

```yaml
environment:
  PENNANT_JWT_SECRET: ${PENNANT_JWT_SECRET}
  DATABASE_URL: postgres://pennant:${POSTGRES_PASSWORD}@db:5432/pennant?sslmode=disable
```

## With TLS (Let's Encrypt)

Add `TLS_AUTO_DOMAIN` and expose port 443. The server handles ACME HTTP-01 challenges automatically, so port 80 must also be reachable during certificate issuance.

```yaml
services:
  pennant:
    build: .
    ports:
      - "80:80"
      - "443:443"
    environment:
      PENNANT_JWT_SECRET: ${PENNANT_JWT_SECRET}
      SQLITE_PATH: /data/pennant.db
      TLS_AUTO_DOMAIN: flags.example.com
    volumes:
      - pennant-data:/data
      - autocert-cache:/root/.cache/autocert
    restart: unless-stopped

volumes:
  pennant-data:
  autocert-cache:
```

## Volume management

```bash
# View volume contents
docker run --rm -v pennant-data:/data alpine ls -la /data

# Backup SQLite database
docker run --rm \
  -v pennant-data:/data \
  -v $(pwd):/backup \
  alpine cp /data/pennant.db /backup/pennant-backup.db

# Restore SQLite database
docker compose down
docker run --rm \
  -v pennant-data:/data \
  -v $(pwd):/backup \
  alpine cp /backup/pennant-backup.db /data/pennant.db
docker compose up -d
```

## Health checks

The Pennant server exposes two probes:

| Endpoint | Purpose |
|---|---|
| `GET /health` | Liveness probe — returns `{"status":"ok","store":"ok"}` or `503` if the store is unreachable |
| `GET /ready` | Readiness probe — used by Kubernetes to gate traffic |

Wait for the service to be healthy before sending traffic:

```bash
until curl -sf http://localhost:8080/health; do sleep 1; done
echo "Pennant is ready"
```

## Scaling notes

- **SQLite:** Single writer only. Do not run multiple Pennant replicas against the same SQLite file.
- **PostgreSQL:** Multiple replicas are safe. Each replica maintains its own SSE hub; a flag update applied to any replica is persisted to Postgres and immediately streamed to that replica's connected SDK clients. SDK clients connecting to different replicas will all converge to the same flag state (eventual consistency within one SSE heartbeat interval).
