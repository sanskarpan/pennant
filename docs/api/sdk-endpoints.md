# SDK Endpoints

SDK endpoints use **SDK key authentication** (not JWT). Pass the SDK key as a Bearer token:

```
Authorization: Bearer sdk-server-default-prod
```

SDK keys are scoped to a project + environment pair. They grant read access to the flag snapshot and write access to the events ingest endpoint. They do not grant access to the management API.

## Snapshot

Retrieve the full flag snapshot for a project environment. SDKs call this on startup to populate their in-memory flag state.

```
GET /sdk/v1/snapshot
```

```bash
curl http://localhost:8080/sdk/v1/snapshot \
  -H "Authorization: Bearer sdk-server-default-prod"
```

**Response `200 OK`:**

```json
{
  "version": 42,
  "checksum": "a3f1b2c4d5e6f7a8",
  "flags": {
    "dark-mode": {
      "key": "dark-mode",
      "type": "boolean",
      "enabled": true,
      "defaultVariation": "off",
      "variations": [
        {"key": "on",  "value": true},
        {"key": "off", "value": false}
      ],
      "rules": [],
      "rollout": [
        {"variation": "on",  "weight": 20000},
        {"variation": "off", "weight": 80000}
      ],
      "targets": [],
      "prerequisites": [],
      "version": 3
    }
  },
  "segments": {
    "beta-testers": {
      "key": "beta-testers",
      "included": ["alice"],
      "excluded": [],
      "rules": [...]
    }
  }
}
```

The snapshot includes all flags and all segments for the authenticated environment. Segments are embedded so evaluation is fully self-contained.

**Conditional fetch with `If-None-Match`:**

The response includes an `ETag` header set to the snapshot checksum. Send `If-None-Match` to avoid re-downloading an unchanged snapshot:

```bash
curl http://localhost:8080/sdk/v1/snapshot \
  -H "Authorization: Bearer sdk-server-default-prod" \
  -H "If-None-Match: \"a3f1b2c4d5e6f7a8\""
```

Returns `304 Not Modified` if the snapshot hasn't changed since the last fetch.

## SSE stream

Subscribe to a real-time SSE stream for incremental flag updates. SDKs open this connection after the initial snapshot load and keep it open for the lifetime of the client.

```
GET /sdk/v1/stream
```

```bash
curl -N http://localhost:8080/sdk/v1/stream \
  -H "Authorization: Bearer sdk-server-default-prod" \
  -H "Accept: text/event-stream"
```

### Event types

**`put` — Full snapshot**

Sent immediately on connection and whenever a delta would be larger than 30% of the full snapshot size.

```
event: put
id: 42
data: {"version":42,"checksum":"a3f1b2c4d5e6f7a8","flags":{...},"segments":{...}}
```

**`patch` — Delta update**

Sent when a flag or segment changes and the diff is smaller than the `delta_threshold`.

```
event: patch
id: 43
data: {"version":43,"changes":[{"op":"replace","path":"/flags/dark-mode/enabled","value":false}]}
```

**`heartbeat` — Keepalive**

Sent every 25 seconds (configurable via `stream.heartbeat_interval` in `config.yaml`) to keep the connection alive through proxies and firewalls.

```
event: heartbeat
data: {}
```

### Reconnect with delta replay

The SSE protocol includes automatic reconnect via the `Last-Event-ID` header. When a client reconnects, it sends:

```
Last-Event-ID: 42
```

The server checks its `EventRing` (a circular buffer of the last 256 events) and replays any events newer than version 42 as delta patches. If the requested version is too old (older than the ring head), the server sends a full `put` event instead.

This means SDK clients recover from brief network interruptions without fetching a full snapshot.

```bash
# Reconnect from event 42
curl -N http://localhost:8080/sdk/v1/stream \
  -H "Authorization: Bearer sdk-server-default-prod" \
  -H "Accept: text/event-stream" \
  -H "Last-Event-ID: 42"
```

### Slow client policy

If a client cannot consume events fast enough and its write buffer fills up, Pennant disconnects it (controlled by `stream.slow_client_policy: disconnect` in `config.yaml`). The client's SSE library will reconnect automatically and receive a fresh snapshot or delta replay. This policy prevents a single slow client from blocking the fan-out goroutine.

## Events (analytics ingest)

Send metric events for A/B test tracking.

```
POST /sdk/v1/events
```

```bash
curl -X POST http://localhost:8080/sdk/v1/events \
  -H "Authorization: Bearer sdk-server-default-prod" \
  -H 'Content-Type: application/json' \
  -d '{
    "events": [
      {
        "type": "metric",
        "userKey": "user-123",
        "eventName": "checkout_completed",
        "value": 1,
        "timestamp": "2024-01-25T12:34:56Z"
      },
      {
        "type": "metric",
        "userKey": "user-456",
        "eventName": "revenue",
        "value": 49.99,
        "timestamp": "2024-01-25T12:35:00Z"
      }
    ]
  }'
```

**Response `202 Accepted`** — Events are queued for batch processing and will be flushed to the database within `analytics.flush_interval` (default 1 second).

### Event fields

| Field | Type | Required | Description |
|---|---|---|---|
| `type` | string | Yes | Always `"metric"` |
| `userKey` | string | Yes | The context key of the user who triggered the event |
| `eventName` | string | Yes | Must match `metricEventName` in the experiment |
| `value` | number | No | Metric value (default `1`). Use `1` for conversions, actual amount for revenue. |
| `timestamp` | ISO 8601 | No | Event time (defaults to server receipt time if omitted) |

Events are **not deduplicated**. If your application sends the same event twice (e.g., on retry), both will be counted. Implement idempotency in your application layer if this matters.

## Observability endpoints

These endpoints do not require authentication.

### Health check

```
GET /health
```

```bash
curl http://localhost:8080/health
```

**Response `200 OK`** (store reachable):

```json
{"status": "ok", "store": "ok"}
```

**Response `503 Service Unavailable`** (store unreachable):

```json
{"status": "degraded", "store": "error: connection refused"}
```

### Readiness probe

```
GET /ready
```

Returns `200 OK` when the server has finished initializing and is ready to handle traffic. Returns `503` during startup or when the store is unavailable. Use this as the Kubernetes `readinessProbe`.

### Prometheus metrics

```
GET /metrics
```

Returns Prometheus exposition format metrics including HTTP request counts, latencies, active SSE connections, event queue depth, and store query latencies.
