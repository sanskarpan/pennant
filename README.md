<div align="center">

# Pennant

**Self-hosted feature flags with local evaluation, real-time streaming, and built-in A/B testing.**

A Go backend, a React admin dashboard, and first-party SDKs for Go and TypeScript — all in a single binary deployable on a $5 VPS or a Kubernetes cluster.

<br/>

![CI](https://github.com/sanskarpan/pennant/actions/workflows/ci.yml/badge.svg)
[![Docs](https://img.shields.io/badge/docs-pennant-blue)](https://sanskarpan.github.io/pennant/)
![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)
![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)
![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript&logoColor=white)

</div>

---

## Why Pennant?

- **No vendor lock-in.** Self-host on a $5 VPS, your own Kubernetes cluster, or `docker compose up`. You own your data and your infrastructure.
- **No evaluation round-trips.** Flags evaluate locally in microseconds using an in-process snapshot. `BoolVariation()` is a hash table lookup and a deterministic bucketing computation — never a network request.
- **Statistically sound A/B testing.** mSPRT gives always-valid p-values that are safe to read at any sample size, not just at a pre-planned endpoint. Pair with chi-square SRM detection to catch broken bucketing before drawing conclusions.
- **Cross-SDK parity guaranteed.** 45+ conformance fixtures run against both the Go and TypeScript SDKs on every CI push. If an edge case passes in Go, it passes in TypeScript.
- **Production-hardened from day one.** Rate limiting, JWT/RBAC, Prometheus metrics, structured logging, health and readiness probes — included, not bolted on.

---

## Screenshots

**Flag list** — manage all flags across projects and environments from one view. Active flags show a green toggle; disabled flags are grey.

![Flag list dashboard showing feature flags with status toggles](docs/images/dashboard.png)

**Targeting rules** — per-environment rule editor. Target by any user attribute (`plan`, `country`, `email`), combine clauses with AND, set percentage rollouts, and define individual override targets — all without a deploy.

![Flag detail page with targeting rules and percentage rollout editor](docs/images/flag-detail.png)

**Quick start** — running locally in under 30 seconds:

```bash
git clone https://github.com/sanskarpan/pennant
cd pennant && cp .env.example .env
# set PENNANT_JWT_SECRET to any 32+ char string
docker compose up
```

Open `http://localhost:8080` — login with `admin@pennant.local` / `admin`. Evaluate a flag from Go:

```go
client, _ := pennant.New(pennant.Options{
    SDKKey:  "sdk-server-default-prod",
    BaseURL: "http://localhost:8080",
})
enabled := client.BoolVariation("my-flag", pennant.Context{Key: "user-123"}, false)
```

Full docs at [sanskarpan.github.io/pennant](https://sanskarpan.github.io/pennant/).

---

## Pennant vs. the alternatives

| | Pennant | LaunchDarkly | Unleash | Flipt |
|---|:---:|:---:|:---:|:---:|
| Self-hosted | Yes | No (SaaS) | Yes | Yes |
| Open source | Yes | No | Yes | Yes |
| Local evaluation (no round-trip) | Yes | Yes (SDK) | Partial | Yes |
| Real-time SSE streaming | Yes | Yes | Yes | No |
| A/B testing built-in | Yes | Yes | Limited | No |
| Always-valid sequential stats (mSPRT) | Yes | Yes | No | No |
| SRM detection | Yes | Yes | No | No |
| PostgreSQL | Yes | -- | Yes | Yes |
| SQLite (single-node) | Yes | -- | No | No |
| Go SDK | Yes | Yes | Yes | Yes |
| TypeScript SDK | Yes | Yes | Yes | Yes |
| Conformance test suite | Yes | -- | No | No |
| Price | Free | $$$$ | Free / Pro | Free / Pro |

---

## Contents

- [How it works](#how-it-works)
- [Performance](#performance)
- [Features](#features)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [API reference](#api-reference)
- [Go SDK](#go-sdk)
- [TypeScript SDK](#typescript-sdk)
- [Evaluation rules](#evaluation-rules)
- [A/B testing](#ab-testing)
- [Development](#development)
- [Testing](#testing)
- [Deployment](#deployment)
- [Security](#security)
- [Project structure](#project-structure)

---

## How it works

```
  Admin dashboard (React)
        |
        | REST /api/v1/*   (JWT, RBAC)
        v
  +--------------------------------------------------+
  |                 Pennant server (Go)              |
  |                                                  |
  |  ConfigStore -----> SnapshotBuilder              |
  |  (SQLite / PostgreSQL / in-memory)               |
  |          |                                       |
  |          +--------> SSE Hub ----------> clients  |
  |                          |                       |
  |                     EventRing (256-slot replay)  |
  +--------------------------------------------------+
        |                          |
        | GET /sdk/v1/snapshot     | GET /sdk/v1/stream  (SSE)
        v                          v
  +----------------------------------------------+
  |           SDK  (Go or TypeScript)            |
  |                                              |
  |   atomic.Pointer[Snapshot]                   |
  |   --> local evaluation  (zero I/O)           |
  +----------------------------------------------+
```

Clients fetch a versioned, checksummed snapshot on startup and subscribe to a Server-Sent Events stream for incremental updates. Every flag mutation rebuilds the snapshot and broadcasts a delta (or a full put, when the diff exceeds 30% of snapshot size) to all connected clients.

Flag evaluation happens entirely in-process. A `BoolVariation` call is a hash table lookup and a deterministic bucketing computation — never a network request.

---

## Performance

The following design decisions keep flag evaluation latency in the low-microsecond range under production load:

**Local evaluation, no network I/O.** The SDK holds a complete copy of all flag rules and evaluates every call in-process. There is no HTTP request on the hot path. Under load testing with 100 concurrent SSE clients and 500 evaluations/sec, p99 evaluation latency is sub-millisecond.

**Lock-free snapshot reads.** The snapshot pointer is stored in `atomic.Pointer[Snapshot]`. Reads on the hot path acquire no locks. The server swaps the pointer atomically on each snapshot rebuild. Goroutines already mid-evaluation continue against the previous snapshot safely.

**Non-blocking SSE fan-out.** The SSE hub sends to each client on a buffered channel. A slow client that cannot keep up is disconnected rather than blocking the broadcast loop. Fast clients are never delayed by slow ones. Disconnected clients reconnect automatically with exponential backoff.

**Delta compression reduces bandwidth.** When only a subset of flags change, the server sends a `patch` event containing only the diff. A `put` (full snapshot) is sent only when the patch would exceed 30% of snapshot size, or when a client reconnects without a `Last-Event-ID`.

**EventRing replay avoids full re-fetches.** A 256-slot circular buffer stores recent delta events. Reconnecting clients send `Last-Event-ID`; the server replays missed events from the ring rather than issuing a full snapshot. This keeps reconnect cost proportional to the number of missed changes, not total flag count.

---

## Features

**Evaluation engine**
- Deterministic SHA-1 bucketing with `BucketScale = 0xFFFFFFFFFFFFFFF` (2^60 - 1)
- 14 clause operators: `in`, `endsWith`, `startsWith`, `contains`, `matchesRegex`, `lessThan`, `lessThanOrEqual`, `greaterThan`, `greaterThanOrEqual`, `semVerEqual`, `semVerLessThan`, `semVerGreaterThan`, `before`, `after`
- Prerequisite chains evaluated recursively before a flag's own rules
- User targeting lists, rule-based targeting, and percentage rollouts
- Segment targeting with explicit included/excluded lists
- Array-valued attributes with OR matching semantics
- Custom `bucketBy` attribute and per-rollout seed overrides
- Weights in 0-100000 range (enables exact 33.333% thirds and 0.001% canaries)

**Real-time delivery**
- SSE streaming hub with non-blocking fan-out (slow clients are disconnected, never block fast ones)
- `EventRing` circular buffer (256 events) for delta replay on reconnect via `Last-Event-ID`
- Canonical JSON checksums — clients detect stale snapshots without full comparison
- Delta compression: patches sent when the diff is under 30% of the full snapshot; full puts otherwise

**Storage**
- In-memory store for testing and CI
- SQLite with WAL mode for single-node production or development
- PostgreSQL with `pgxpool` connection pooling and JSONB columns for multi-node production
- `NewStore()` factory selects the backend from environment variables

**Auth and access control**
- JWT-based authentication (HS256, 1-hour access tokens)
- Single-use rotating refresh tokens (7-day TTL, invalidated on first use)
- Four-tier RBAC: `owner` > `admin` > `editor` > `viewer`
- Login rate-limited to 5 attempts per minute per IP
- SDK keys generated with `crypto/rand`, full value returned only at creation time

**Observability**
- Prometheus metrics on `/metrics`: active SSE connections, slow-client disconnects, flag evaluation counters and histograms, HTTP request counts and latencies, snapshot build duration
- Structured JSON request logging with `X-Request-ID` correlation
- `/health` probes the store and returns 503 if unreachable
- `/ready` Kubernetes readiness probe

**A/B testing**
- mSPRT (mixture Sequential Probability Ratio Test) — always-valid p-values, safe to check at any sample size
- Two-proportion z-test with pooled SE for p-value and unpooled SE for confidence intervals
- Welford online algorithm for streaming mean/variance without storing raw events
- Chi-square sample ratio mismatch (SRM) detection
- Statistical power and sample size calculator

**Conformance**
- 45+ JSON fixtures covering every clause operator, rollout, prerequisite, segment, and target scenario
- Both the Go and TypeScript SDKs run against the identical fixture set — evaluation parity is enforced by CI

---

## Quick start

### Docker Compose (recommended)

```bash
git clone https://github.com/sanskarpan/pennant
cd pennant
cp .env.example .env
# Set PENNANT_JWT_SECRET to a random string of at least 32 chars
docker compose up
```

The server starts on `http://localhost:8080`. The admin dashboard is served at the same address.

Default credentials: `admin@pennant.local` / `admin`

> Change the default password immediately after first login.

### Binary

```bash
go build -o pennant-server ./cmd/server
PENNANT_JWT_SECRET=change-me SQLITE_PATH=./pennant.db ./pennant-server
```

### Build from source with the frontend

```bash
# Build the frontend (requires Bun)
cd frontend && bun install && bun run build && cd ..

# Build the server (CGO required for SQLite)
CGO_ENABLED=1 go build -o pennant-server ./cmd/server

PENNANT_JWT_SECRET=change-me SQLITE_PATH=./pennant.db ./pennant-server
```

---

## Configuration

All settings are passed as environment variables. See `.env.example` for the full list.

| Variable | Default | Description |
|---|---|---|
| `PENNANT_JWT_SECRET` | insecure dev default | HS256 signing secret. **Required in production.** Minimum 32 chars. A warning is logged if unset. |
| `PENNANT_CORS_ORIGINS` | `""` (allow all) | Comma-separated list of allowed CORS origins. Empty allows all — suitable for local dev only. |
| `PENNANT_ADMIN_TOKEN` | `""` | Legacy token-based admin auth. Leave empty when JWT is configured. |
| `SQLITE_PATH` | `""` | Path to the SQLite database file. If unset and `DATABASE_URL` is also unset, the in-memory store is used. |
| `DATABASE_URL` | `""` | PostgreSQL DSN (`postgres://user:pass@host:5432/dbname?sslmode=disable`). Takes priority over `SQLITE_PATH`. |
| `TLS_CERT_FILE` | `""` | Path to TLS certificate PEM. Pair with `TLS_KEY_FILE`. |
| `TLS_KEY_FILE` | `""` | Path to TLS private key PEM. |
| `TLS_AUTO_DOMAIN` | `""` | Domain for Let's Encrypt autocert (e.g. `flags.example.com`). Starts an HTTP to HTTPS redirect on `:80`. |

**Storage selection order:** `DATABASE_URL` then `SQLITE_PATH` then in-memory.

**TLS mode selection order:** `TLS_AUTO_DOMAIN` then `TLS_CERT_FILE`+`TLS_KEY_FILE` then plain HTTP.

---

## API reference

All management endpoints require a `Bearer` JWT token obtained from `POST /auth/login`.

### Authentication

```
POST   /auth/login      { "email": "...", "password": "..." }
                        --> { "accessToken": "...", "refreshToken": "..." }

POST   /auth/refresh    { "refreshToken": "..." }
                        --> { "accessToken": "...", "refreshToken": "..." }

GET    /auth/me         --> { "userID": "...", "email": "...", "role": "..." }
```

`/auth/login` is rate-limited to 5 requests per minute per IP.

### Projects

```
GET    /api/v1/projects
POST   /api/v1/projects                        { "key": "my-app", "name": "My App" }

GET    /api/v1/projects/{projectKey}
PUT    /api/v1/projects/{projectKey}
DELETE /api/v1/projects/{projectKey}

GET    /api/v1/projects/{projectKey}/audit?limit=100   (max 500)
```

### Environments

```
GET    /api/v1/projects/{projectKey}/environments
POST   /api/v1/projects/{projectKey}/environments

PUT    /api/v1/projects/{projectKey}/environments/{envKey}
DELETE /api/v1/projects/{projectKey}/environments/{envKey}
```

### SDK keys

```
GET    /api/v1/projects/{projectKey}/environments/{envKey}/sdk-keys
       --> [{ "id": "...", "type": "server|client", "maskedValue": "sdk-serv****abcd" }]

POST   /api/v1/projects/{projectKey}/environments/{envKey}/sdk-keys
       --> { ..., "value": "sdk-server-xxxxxxxxxxxxxxxxxxxx" }   <- full value returned ONCE only

DELETE /api/v1/projects/{projectKey}/environments/{envKey}/sdk-keys/{keyID}
```

The full SDK key value is only returned on `POST`. Subsequent `GET` requests return a masked value. Store the key securely on creation.

### Flags

```
GET    /api/v1/projects/{projectKey}/environments/{envKey}/flags
POST   /api/v1/projects/{projectKey}/environments/{envKey}/flags

GET    /api/v1/projects/{projectKey}/environments/{envKey}/flags/{flagKey}
PUT    /api/v1/projects/{projectKey}/environments/{envKey}/flags/{flagKey}
DELETE /api/v1/projects/{projectKey}/environments/{envKey}/flags/{flagKey}
```

Flag body:

```json
{
  "key": "dark-mode",
  "name": "Dark Mode",
  "type": "boolean",
  "variations": [
    { "key": "on",  "value": true },
    { "key": "off", "value": false }
  ],
  "config": {
    "enabled": true,
    "offVariation": "off",
    "fallthrough": { "variation": "off" },
    "targets": [
      { "variation": "on", "values": ["user-123", "user-456"] }
    ],
    "rules": [
      {
        "clauses": [
          { "attribute": "plan", "operator": "in", "values": ["pro", "enterprise"] }
        ],
        "serve": { "variation": "on" }
      }
    ]
  }
}
```

Mutating a flag triggers an immediate snapshot rebuild and an SSE event to all connected clients.

### Segments

```
GET    /api/v1/projects/{projectKey}/segments
POST   /api/v1/projects/{projectKey}/segments

GET    /api/v1/projects/{projectKey}/segments/{segmentKey}
PUT    /api/v1/projects/{projectKey}/segments/{segmentKey}
DELETE /api/v1/projects/{projectKey}/segments/{segmentKey}
```

Segment body:

```json
{
  "key": "internal-beta",
  "name": "Internal Beta",
  "included": ["user-001", "user-002"],
  "excluded": ["user-banned"],
  "rules": [
    {
      "clauses": [
        { "attribute": "email", "operator": "endsWith", "values": ["@example.com"] }
      ]
    }
  ]
}
```

`excluded` beats `included`. A user in both lists is treated as excluded.

### Experiments

```
GET    /api/v1/projects/{projectKey}/experiments
POST   /api/v1/projects/{projectKey}/experiments

GET    /api/v1/projects/{projectKey}/experiments/{experimentKey}/results
```

### SDK endpoints (SDK key auth)

```
GET    /sdk/v1/snapshot            Authorization: Bearer <sdk-key>
GET    /sdk/v1/stream              Authorization: Bearer <sdk-key>   (SSE)
POST   /sdk/v1/events              Authorization: Bearer <sdk-key>
```

The stream endpoint accepts `Last-Event-ID` for delta replay from the `EventRing`. SSE events are one of:

| Event type | Payload | Meaning |
|---|---|---|
| `put` | Full snapshot JSON | Full replace |
| `patch` | Delta JSON | Apply diff to current snapshot |
| `heartbeat` | `{}` | Keep-alive (no state change) |

### Health and observability

```
GET  /health     --> 200 { "status": "ok", "store": "ok" }   or 503 if store unreachable
GET  /ready      --> 200 "ok"
GET  /metrics    --> Prometheus exposition format
```

---

## Go SDK

### Installation

```bash
go get github.com/sanskarpan/pennant/sdk/go/pennant
```

### Usage

```go
package main

import (
    "fmt"
    "log"

    "pennant/sdk/go/pennant"
    "pennant/internal/model"
)

func main() {
    client, err := pennant.New(pennant.Options{
        SDKKey:  "sdk-server-your-key-here",
        BaseURL: "http://localhost:8080",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := model.Context{
        Kind: "user",
        Key:  "user-123",
        Attributes: map[string]any{
            "plan":  "pro",
            "email": "alice@example.com",
        },
    }

    // Boolean flag
    darkMode := client.BoolVariation("dark-mode", ctx, false)
    fmt.Println("dark-mode:", darkMode)

    // String flag
    theme := client.StringVariation("ui-theme", ctx, "default")
    fmt.Println("ui-theme:", theme)

    // Numeric flag
    timeout := client.Float64Variation("request-timeout-ms", ctx, 5000.0)
    fmt.Println("timeout:", timeout)

    // JSON flag (arbitrary struct)
    var config struct {
        MaxRetries int    `json:"maxRetries"`
        Region     string `json:"region"`
    }
    if err := client.JSONVariation("service-config", ctx, &config); err != nil {
        log.Println("flag not found, using defaults")
    }
}
```

The client connects over SSE on `New()` and blocks until the first snapshot arrives (default 5 second timeout). After that, all `*Variation` calls are synchronous, in-memory operations — no locks, no I/O. The snapshot pointer is updated via `atomic.Pointer[Snapshot]`.

If the server is unreachable at startup, `New()` returns a non-nil client that evaluates all flags to their default values and self-heals when the server comes back.

### Reconnect behaviour

The client reconnects automatically with exponential backoff on SSE disconnections. It sends `Last-Event-ID` on each reconnect so the server can replay missed delta events from the `EventRing`.

---

## TypeScript SDK

### Installation

```bash
bun add @sanskarpan/pennant-ts   # or: npm install @sanskarpan/pennant-ts
```

### Usage (browser or Node)

```typescript
import { PennantClient } from '@sanskarpan/pennant-ts'

const client = new PennantClient({
  sdkKey: 'sdk-client-your-key-here',
})

await client.init('http://localhost:8080')

const ctx = {
  kind: 'user',
  key: 'user-123',
  attributes: {
    plan: 'pro',
    email: 'alice@example.com',
  },
}

// Boolean
const darkMode: boolean = client.boolVariation('dark-mode', ctx, false)

// String
const theme: string = client.stringVariation('ui-theme', ctx, 'default')

// JSON (typed)
interface ServiceConfig { maxRetries: number; region: string }
const config = client.jsonVariation<ServiceConfig>('service-config', ctx, {
  maxRetries: 3,
  region: 'us-east-1',
})
```

### BigInt bucketing

SHA-1 produces a 160-bit hash. Bucketing uses the first 60 bits. JavaScript's `Number` type is a 64-bit float with only 53 bits of integer precision — using it for the bucket comparison silently loses bits and diverges from the Go implementation for some user keys. The TypeScript SDK uses `BigInt` throughout:

```typescript
const BUCKET_SCALE = BigInt('0xFFFFFFFFFFFFFFF')  // 2^60 - 1
// bucket = BigInt(sha1_prefix_60_bits) / BUCKET_SCALE
```

This is enforced by the conformance suite — both SDKs must produce identical results for all 45+ fixtures.

---

## Evaluation rules

Flags are evaluated in strict priority order. The first matching rule wins.

```
1. Flag is OFF
   --> Return offVariation (or default if none set)

2. Prerequisite flags
   --> Evaluate each prerequisite recursively
   --> If any fails (wrong variation or itself off) --> return offVariation

3. Individual targets
   --> If user key appears in targets[].values --> return that variation

4. Rules (evaluated in order, first match wins)
   --> All clauses in a rule must match (AND semantics)
   --> Rule can serve a fixed variation or a weighted rollout

5. Fallthrough
   --> If no rule matched --> serve the fallthrough variation or rollout
```

### Clause operators

| Operator | Type | Example |
|---|---|---|
| `in` | String, number | `plan in ["pro", "enterprise"]` |
| `endsWith` | String | `email endsWith "@acme.com"` |
| `startsWith` | String | `email startsWith "admin"` |
| `contains` | String | `name contains "test"` |
| `matchesRegex` | String | `email matchesRegex ".*@(acme\|widgets)\.com"` |
| `lessThan` | Number | `age lessThan 18` |
| `lessThanOrEqual` | Number | `score lessThanOrEqual 100` |
| `greaterThan` | Number | `score greaterThan 0` |
| `greaterThanOrEqual` | Number | `version greaterThanOrEqual 2` |
| `semVerEqual` | Semver string | `appVersion semVerEqual "2.1.0"` |
| `semVerLessThan` | Semver string | `appVersion semVerLessThan "3.0.0"` |
| `semVerGreaterThan` | Semver string | `appVersion semVerGreaterThan "1.9.9"` |
| `before` | RFC3339 datetime | `signupDate before "2024-01-01T00:00:00Z"` |
| `after` | RFC3339 datetime | `signupDate after "2023-06-01T00:00:00Z"` |

All operators can be negated by setting `"negate": true` on the clause.

When a user attribute is an array (e.g. `tags: ["beta", "internal"]`), the `in`, `endsWith`, `startsWith`, and `contains` operators match if **any** element matches (OR semantics across elements).

### Percentage rollouts

Rollout weights are integers in 0-100000. A weight of 33333 is exactly 33.333%; a weight of 1 is 0.001%. Weights across all variations in a rollout must sum to 100000.

The bucket is computed as:

```
bucket = SHA1(flagKey + "." + bucketByAttr)[0:15 hex chars] / 0xFFFFFFFFFFFFFFF
```

The user is placed in the variation whose cumulative weight range contains their bucket. The same user always gets the same variation for the same flag and seed. Changing the seed reassigns users without changing their key.

---

## A/B testing

### Creating an experiment

```bash
curl -X POST http://localhost:8080/api/v1/projects/my-app/experiments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "key":                "checkout-v2",
    "name":               "Checkout flow v2",
    "flagKey":            "checkout-redesign",
    "controlVariation":   "control",
    "treatmentVariation": "treatment",
    "metric":             "purchase_completed"
  }'
```

### Tracking events

Send events from your server via the SDK endpoint:

```bash
curl -X POST http://localhost:8080/sdk/v1/events \
  -H "Authorization: Bearer $SDK_KEY" \
  -H "Content-Type: application/json" \
  -d '[{
    "type":      "metric",
    "flagKey":   "checkout-redesign",
    "variation": "treatment",
    "userKey":   "user-456",
    "metric":    "purchase_completed",
    "value":     1
  }]'
```

### Reading results

```bash
curl http://localhost:8080/api/v1/projects/my-app/experiments/checkout-v2/results \
  -H "Authorization: Bearer $TOKEN"
```

```json
{
  "experimentKey": "checkout-v2",
  "control": {
    "variation": "control",
    "count":     4821,
    "mean":      0.0412,
    "variance":  0.0395
  },
  "treatment": {
    "variation":    "treatment",
    "count":        4903,
    "mean":         0.0531,
    "lift":         0.0119,
    "liftPercent":  28.88,
    "pValue":       0.00031,
    "ciLow":        0.0051,
    "ciHigh":       0.0187,
    "msprtPValue":  0.00028,
    "significant":  true
  },
  "srm": {
    "chiSquare": 0.69,
    "pValue":    0.407,
    "detected":  false
  }
}
```

**Two p-values are returned:**
- `pValue` — fixed-horizon two-proportion z-test. Valid only at the pre-planned sample size.
- `msprtPValue` — mSPRT always-valid sequential p-value. Safe to act on at any sample size.

**SRM check:** If `srm.detected` is `true`, the experiment assignment is unbalanced. Results are not trustworthy — investigate the bucketing logic before drawing conclusions.

---

## Development

### Prerequisites

| Tool | Version | Purpose |
|---|---|---|
| Go | 1.22+ | Backend |
| Bun | 1.x | Frontend and TypeScript SDK |
| golangci-lint | 1.59+ | Linting |
| Docker | 24+ | Integration tests with Postgres |

```bash
git clone https://github.com/sanskarpan/pennant
cd pennant

# Backend
go build ./...

# Frontend
cd frontend && bun install && bun run dev

# TypeScript SDK
cd sdk/ts && bun install && bun test
```

### Makefile targets

```
make run           Start the server (in-memory store, no auth)
make test          Run all Go tests
make test-race     Run all Go tests with -race -count=3
make conformance   Run conformance suite (Go + TypeScript)
make lint          golangci-lint
make frontend-dev  Start Vite dev server with API proxy to :8080
make loadgen       Run the load generator (CLIENTS=N)
```

### Environment for local dev

```bash
# Minimal — uses in-memory store, no TLS, JWT secret warned but defaults
go run ./cmd/server

# With persistent store
SQLITE_PATH=./dev.db go run ./cmd/server

# With JWT
PENNANT_JWT_SECRET=dev-secret-at-least-32-chars SQLITE_PATH=./dev.db go run ./cmd/server
```

The server seeds a default project (`default`), environment (`production`), and two SDK keys (`sdk-server-default-prod`, `sdk-client-default-prod`) on first start.

The admin user is created at `admin@pennant.local` with password `admin` on every start. Duplicate-user errors on subsequent starts are silently ignored.

---

## Testing

### Go tests

```bash
# All packages
go test ./...

# With race detector (mandatory in CI)
go test ./... -race -count=3

# Integration tests (requires a Postgres instance)
DATABASE_URL=postgres://pennant:pennant@localhost:5432/pennant?sslmode=disable \
  go test ./internal/store/... -tags=integration

# Conformance suite only
go test ./... -run TestConformance -v
```

### TypeScript SDK tests

```bash
cd sdk/ts
bun test                        # 89 tests
bun test conformance.test.ts    # Cross-language fixture suite
```

### Frontend build check

```bash
cd frontend
bun run build   # tsc + Vite build; fails on any type error
```

### Load testing

```bash
# 100 concurrent SSE clients, 500 evaluations/sec
make loadgen CLIENTS=100
```

---

## Deployment

### Docker Compose (SQLite, single node)

```bash
cp .env.example .env
# Fill in PENNANT_JWT_SECRET
docker compose up -d
```

### Docker Compose (PostgreSQL, multi-node)

Uncomment the `DATABASE_URL` line in `docker-compose.yml`:

```yaml
environment:
  - DATABASE_URL=postgres://pennant:pennant@postgres:5432/pennant?sslmode=disable
```

```bash
docker compose up -d
```

### Building the Docker image

```bash
docker build -t pennant:latest .
```

The Dockerfile uses a three-stage build:
1. `oven/bun:1` — builds the React frontend
2. `golang:1.22-alpine` with `gcc` and `musl-dev` — compiles the Go binary with `CGO_ENABLED=1` (required for SQLite)
3. `alpine:3.19` with `ca-certificates` and `sqlite-libs` — minimal runtime image

### Kubernetes

Expose the deployment with two probes:

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /ready
    port: 8080
  initialDelaySeconds: 2
  periodSeconds: 5
```

For multi-replica deployments, use `DATABASE_URL` (PostgreSQL) — the in-memory and SQLite stores are not shared across pods.

Set resource requests to account for the SSE hub holding one goroutine per connected client:

```yaml
resources:
  requests:
    memory: "128Mi"
    cpu: "100m"
  limits:
    memory: "512Mi"
    cpu: "500m"
```

### TLS

**Reverse proxy (recommended):** Run Pennant without TLS and terminate at nginx or a cloud load balancer. No `TLS_*` variables needed.

**Automatic (Let's Encrypt):**

```bash
TLS_AUTO_DOMAIN=flags.example.com ./pennant-server
```

Certificates are cached in `./tls-cache/`. Port 80 must be reachable for the ACME HTTP-01 challenge. The server redirects HTTP to HTTPS automatically.

**Static certificate:**

```bash
TLS_CERT_FILE=/etc/ssl/certs/pennant.pem \
TLS_KEY_FILE=/etc/ssl/private/pennant-key.pem \
./pennant-server
```

---

## Security

### Authentication flow

```
Client                          Server
  |                               |
  |--> POST /auth/login --------> |  validates password (bcrypt)
  |<-- { accessToken (1h),        |
  |      refreshToken (7d) } <--- |
  |                               |
  |--> GET /api/v1/... ---------> |  validates JWT (HS256)
  |                               |  checks role (RequireRole middleware)
  |                               |
  |--> POST /auth/refresh ------> |  validates refresh token (single-use)
  |                               |  invalidates old token immediately
  |<-- { new accessToken,         |
  |      new refreshToken } <---- |
```

Refresh tokens are single-use. When a refresh token is consumed it is deleted from the store and replaced with a new one. If an attacker intercepts and uses a refresh token before the legitimate client, the client's next refresh request will fail (401), alerting the user that their session was compromised.

### Rate limiting

| Endpoint | Limit | Header on reject |
|---|---|---|
| All (per IP) | 1000 req/s, burst 2000 | `Retry-After: 1` |
| SDK endpoints (per SDK key) | 500 req/s, burst 1000 | `Retry-After: 1` |
| `POST /auth/login` | 5 req/min per IP | `Retry-After: 60` |

SSE streaming connections (`/sdk/v1/stream`) are excluded from per-request rate limiting — they are long-lived connections, not per-request calls.

### Request hardening

- All request bodies capped at 1 MB via `MaxBytesReader`
- `X-Request-ID` injected on every request for distributed tracing
- Recovery middleware catches panics and returns 500 instead of crashing the process

### SDK keys

- Generated with `crypto/rand` (16 bytes, 32 hex chars)
- Full value returned only on `POST /sdk-keys`. Never stored in plaintext after creation — only the masked form (`sdk-serv****abcd`) appears in subsequent `GET` responses
- Server-type and client-type keys are issued separately. Client keys should be considered semi-public (embedded in browser apps); server keys must be kept secret

### Production checklist

- [ ] Set `PENNANT_JWT_SECRET` to a randomly generated string of at least 32 characters
- [ ] Set `PENNANT_CORS_ORIGINS` to your specific frontend origin(s)
- [ ] Change the default admin password immediately after first login
- [ ] Use `DATABASE_URL` (PostgreSQL) for multi-replica deployments
- [ ] Run behind a reverse proxy or set `TLS_AUTO_DOMAIN` / `TLS_CERT_FILE`
- [ ] Rotate SDK keys on a schedule; revoke compromised keys immediately
- [ ] Mount `/metrics` only on an internal port or protect with network policy

---

## Project structure

```
pennant/
├── cmd/
│   ├── server/          Entry point: wires all components, TLS, graceful shutdown
│   └── loadgen/         Configurable load generator for SSE and eval benchmarking
│
├── internal/
│   ├── model/           Core types: Flag, Rule, Clause, Segment, Context, Rollout
│   ├── eval/            Evaluation engine: bucketing, operators, clause and flag eval
│   ├── store/           ConfigStore interface + SQLite, PostgreSQL, in-memory backends
│   ├── snapshot/        Canonical JSON serialization, snapshot builder, delta computation
│   ├── stream/          SSE hub, EventRing circular buffer, non-blocking fan-out
│   ├── events/          Internal non-blocking publish/subscribe event bus
│   ├── analytics/       Event ingestion pipeline with configurable batch flushing
│   ├── stats/           z-test, mSPRT, Welford algorithm, SRM detection, sample size
│   ├── experiment/      Experiment store and result computation engine
│   ├── auth/            JWT service, RBAC roles, bcrypt passwords, user stores
│   ├── sdkauth/         SDK key registry and per-request authenticator
│   ├── ratelimit/       Token bucket middleware (IP, SDK key, login)
│   ├── metrics/         Prometheus instrumentation, InstrumentHandler with Flusher
│   ├── api/             chi router, all REST and SDK HTTP handlers
│   └── integration/     Full-stack integration tests (real HTTP server)
│
├── conformance/         45+ JSON fixtures for cross-SDK evaluation parity
├── conformance_test.go  Go fixture runner
│
├── sdk/
│   ├── go/pennant/      Go client SDK (atomic snapshot, SSE reconnect, type-safe eval)
│   └── ts/src/          TypeScript SDK (BigInt bucketing, SSE client, conformance tests)
│
├── frontend/src/
│   ├── App.tsx                      Root: auth gate, project/env switcher, 5-tab layout
│   ├── lib/api.ts                   Type-safe fetch client with JWT injection and refresh retry
│   ├── lib/auth.ts                  Token storage, login, logout, refresh helpers
│   └── components/
│       ├── FlagList.tsx             Flag list with create form and enable toggle
│       ├── FlagDetail.tsx           Targeting rules editor (prereqs, targets, rules, rollout)
│       ├── ExperimentDashboard.tsx  Results table, Recharts bar chart, SRM warning
│       ├── SegmentManager.tsx       Two-panel segment editor with dirty-state save
│       ├── PropagationMonitor.tsx   Real-time SSE event stream display
│       ├── AuditLog.tsx             Paginated project audit log
│       └── LoginPage.tsx            Email/password login form
│
├── Dockerfile           Three-stage build: Bun -> Go (CGO) -> Alpine
├── docker-compose.yml   Pennant + PostgreSQL with health checks
├── .env.example         All configuration variables documented
└── Makefile             run, test, test-race, conformance, lint, frontend-dev, loadgen
```

---

## License

MIT
