# Contributing

Thank you for contributing to Pennant. This page covers how to run the project locally, execute the test suite, and understand the conformance fixture system.

## Development setup

### Prerequisites

- Go 1.22+ with CGO enabled (required for SQLite)
- Bun 1.x (for the frontend)
- `golangci-lint` (for linting)
- Docker (optional, for integration tests with PostgreSQL)

### Clone and run

```bash
git clone https://github.com/sanskarpan/pennant
cd pennant

# Start the server with in-memory storage (no database required)
make run

# Or with SQLite persistence
SQLITE_PATH=./dev.db make run
```

The server starts on `http://localhost:8080`. Login with `admin@pennant.local` / `admin`.

### Frontend development

The frontend (React + Vite) runs on its own dev server with API proxying:

```bash
make frontend-dev
# Opens http://localhost:5173
# API requests are proxied to :8080
```

Changes to the frontend are hot-reloaded. The backend must be running separately (`make run` in another terminal).

## Running tests

### All tests

```bash
make test
```

Runs `go test ./...` with standard flags. Uses in-memory storage.

### Race detector

```bash
make test-race
```

Runs `go test ./... -race -count=3`. Essential for SSE hub and analytics pipeline code. Always run this before opening a PR that touches concurrency.

### Conformance suite

```bash
make conformance
```

Runs the evaluation engine against all JSON fixtures in `conformance/`:
1. Executes `go test -run TestConformance ./...` against the Go engine.
2. Executes the TypeScript SDK test runner against the same fixtures.
3. Both must pass for the suite to succeed.

### Integration tests

```bash
go test ./internal/integration/... -tags integration
```

Requires Docker. Spins up a real PostgreSQL instance and runs the full server against it. These tests cover flag CRUD, SSE streaming, and analytics ingestion end-to-end.

### Load testing

```bash
# Start 50 concurrent SSE clients
make loadgen CLIENTS=50

# Or run the binary directly
PENNANT_URL=http://localhost:8080 \
PENNANT_SDK_KEY=sdk-server-default-prod \
./loadgen -clients 50 -flags 100 -updates-per-sec 10
```

The load generator is in `cmd/loadgen/`. It creates SDK clients, subscribes to the SSE stream, and simulates flag updates from a concurrent writer. Output includes client reconnect counts and p50/p95/p99 snapshot latencies.

## Conformance fixture format

Each fixture in `conformance/` is a subdirectory with two files:

**`input.json`:**

```json
{
  "snapshot": {
    "version": 1,
    "flags": {
      "my-flag": {
        "key": "my-flag",
        "type": "boolean",
        "enabled": true,
        "defaultVariation": "off",
        "variations": [
          {"key": "on",  "value": true},
          {"key": "off", "value": false}
        ],
        "rules": [
          {
            "id": "rule-1",
            "clauses": [
              {"attribute": "plan", "op": "in", "values": ["beta"]}
            ],
            "variation": "on"
          }
        ],
        "rollout": [],
        "targets": [],
        "prerequisites": []
      }
    },
    "segments": {}
  },
  "context": {
    "key": "user-123",
    "attributes": {
      "plan": "beta"
    }
  },
  "flagKey": "my-flag"
}
```

**`expected.json`:**

```json
{
  "variation": "on",
  "value": true,
  "reason": "rule:rule-1"
}
```

### Adding a new fixture

1. Create a new subdirectory under `conformance/`, e.g. `conformance/rule-semver-gt/`.
2. Write `input.json` with a snapshot, context, and flag key that exercises the behavior you want to test.
3. Run `go test -run TestConformance ./...` — it will print the actual result. If it matches your expectation, create `expected.json` with that result.
4. Run `make conformance` to verify both Go and TypeScript produce the expected output.

### Fixture naming conventions

| Prefix | Covers |
|---|---|
| `rule-` | Rule matching with specific operators |
| `rollout-` | Percentage rollout bucketing |
| `segment-` | Segment membership |
| `prereq-` | Prerequisite chains |
| `target-` | Individual target overrides |
| `disabled-` | Off flag behavior |
| `bigint-` | Cases where float64 bucketing would give wrong results |

## Code structure

```
cmd/
  server/      Entry point, HTTP server setup
  loadgen/     Load testing binary
internal/
  api/         HTTP handlers, router
  auth/        JWT, RBAC, user management
  analytics/   Event ingestion pipeline
  eval/        Flag evaluation engine (bucket.go, clause.go, evaluate.go)
  events/      Internal pub/sub bus
  experiment/  Experiment store and results engine
  metrics/     Prometheus instrumentation
  model/       Core data types
  ratelimit/   Token bucket middleware
  sdkauth/     SDK key registry
  snapshot/    Snapshot builder and delta computation
  stats/       Statistical functions (z-test, mSPRT, Welford, SRM)
  store/       Storage backends (memory, sqlite, postgres)
  stream/      SSE hub and EventRing
sdk/
  go/pennant/  Go SDK
  ts/src/      TypeScript SDK
conformance/   Evaluation conformance fixtures
frontend/      React dashboard (Vite + Bun)
```

## Pull request guidelines

1. Run `make test-race` and `make conformance` locally before pushing.
2. Add a conformance fixture for any new evaluation behavior.
3. Update the relevant docs page in `docs/` if your change affects documented behavior.
4. Keep PRs focused — one logical change per PR.
5. Include a description of what changed and why.

## Linting

```bash
make lint
```

Uses `golangci-lint` with the config in `.golangci.yml`. The CI pipeline runs lint on every push.
