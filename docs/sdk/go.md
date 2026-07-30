# Go SDK

The Go SDK evaluates feature flags locally using an in-memory snapshot. Evaluation calls (`BoolVariation`, `StringVariation`, etc.) are pure in-memory operations — no network I/O on the hot path.

## Installation

```bash
go get pennant/sdk/go/pennant
```

The SDK is in the `sdk/go/pennant` directory of the Pennant repository.

## Creating a client

```go
import "pennant/sdk/go/pennant"

client, err := pennant.NewClient(pennant.Config{
    BaseURL: "http://localhost:8080",
    SDKKey:  "sdk-server-default-prod",
})
if err != nil {
    log.Fatal(err)
}
defer client.Close()
```

`NewClient` performs these steps synchronously:
1. Fetches the initial flag snapshot via `GET /sdk/v1/snapshot`.
2. Starts a background goroutine that maintains an SSE connection to `GET /sdk/v1/stream`.

The client is ready to evaluate flags immediately after `NewClient` returns. If the initial snapshot fetch fails, `NewClient` returns an error and the background goroutine is not started.

### Config options

| Field | Type | Default | Description |
|---|---|---|---|
| `BaseURL` | string | (required) | Pennant server base URL, no trailing slash |
| `SDKKey` | string | (required) | SDK key for the target environment |
| `HTTPClient` | `*http.Client` | default client | Custom HTTP client (e.g., with proxy, custom TLS) |

## Evaluation context

```go
ctx := pennant.EvalContext{
    Key: "user-123",                      // Required: stable identifier
    Attributes: map[string]any{           // Optional: arbitrary key-value pairs
        "plan":       "enterprise",
        "country":    "US",
        "appVersion": "2.3.1",
        "score":      145.5,
        "active":     true,
    },
}
```

`Key` is the entity being evaluated (typically a user ID, device ID, or session ID). It determines bucket assignment for percentage rollouts — keep it stable.

## Variation methods

### BoolVariation

```go
enabled := client.BoolVariation("my-flag", ctx, false)
// Returns: bool
// Third argument: default value returned if the flag is not found or evaluation errors
```

### StringVariation

```go
theme := client.StringVariation("ui-theme", ctx, "default")
// Returns: string
```

### IntVariation

```go
limit := client.IntVariation("rate-limit", ctx, 100)
// Returns: int
```

### JSONVariation

```go
var config MyConfig
err := client.JSONVariation("feature-config", ctx, defaultConfig, &config)
// Unmarshals the flag's JSON value into the provided pointer
// Returns: error (nil on success)
```

### ExplainVariation

Get the variation value plus a reason explaining which step of the evaluation pipeline produced it:

```go
enabled, reason := client.ExplainBoolVariation("my-flag", ctx, false)
// reason values:
//   "off"           — flag is disabled
//   "prerequisite"  — prerequisite not met
//   "target"        — matched individual target
//   "rule:<id>"     — matched rule with given ID
//   "rollout"       — bucket assigned by percentage rollout
//   "default"       — fell through to defaultVariation
//   "not-found"     — flag key does not exist in the snapshot
```

Use `ExplainVariation` in development and testing to understand unexpected evaluation results. Avoid it on the hot path in production (though it is not significantly more expensive than a regular evaluation).

## Tracking events (for A/B testing)

```go
client.Track("checkout_completed", ctx, 1.0)
client.Track("revenue", ctx, 49.99)
```

Events are queued in memory and flushed asynchronously. `Track` never blocks.

## Atomic snapshot updates

The SDK stores the flag snapshot in an `atomic.Pointer[Snapshot]`. When the SSE stream delivers an update:

1. The background goroutine receives the event.
2. For `patch` events: it applies the delta to a copy of the current snapshot.
3. For `put` events: it sets the new snapshot directly.
4. It atomically swaps the pointer: `s.snapshot.Store(newSnapshot)`.

Ongoing `BoolVariation` calls that loaded the pointer before the swap continue against the old snapshot. New calls after the swap use the new snapshot. There is no mutex on the evaluation hot path — reads are always non-blocking.

## Reconnect behavior

The background SSE goroutine implements exponential backoff with jitter on reconnect:

1. Connection drops (network error, server restart, etc.)
2. Client sends `Last-Event-ID: <last received version>` on reconnect.
3. Server replays events newer than that version from the `EventRing`.
4. If the last version is too old (> 256 events behind), the server sends a full `put` event.
5. SDK applies the new snapshot atomically.

During a reconnect window, the SDK continues evaluating flags against the last known snapshot. Flag evaluations do not fail or block during reconnects.

Backoff intervals: 1s, 2s, 4s, 8s, 16s (capped at 60s). Jitter is applied to prevent thundering-herd reconnects after a server restart.

## Closing the client

```go
client.Close()
```

`Close` cancels the background SSE goroutine and flushes any pending analytics events. Call it when your application shuts down. After `Close`, all variation calls return their default values.

## Testing with the SDK

For unit tests, you can instantiate the SDK with an in-memory Pennant server or bypass the SDK entirely:

```go
// Option 1: Use a test Pennant server (integration tests)
ts := httptest.NewServer(pennantHandler)
client, _ := pennant.NewClient(pennant.Config{
    BaseURL: ts.URL,
    SDKKey:  "test-key",
})

// Option 2: Mock at the application layer
// Wrap SDK calls behind an interface and inject a test double
type FlagClient interface {
    BoolVariation(key string, ctx pennant.EvalContext, defaultVal bool) bool
}
```

## Full example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"

    "pennant/sdk/go/pennant"
)

func main() {
    client, err := pennant.NewClient(pennant.Config{
        BaseURL:    "https://flags.example.com",
        SDKKey:     "sdk-server-default-prod",
        HTTPClient: &http.Client{Timeout: 10 * time.Second},
    })
    if err != nil {
        log.Fatalf("failed to init pennant client: %v", err)
    }
    defer client.Close()

    // Serve requests
    http.HandleFunc("/checkout", func(w http.ResponseWriter, r *http.Request) {
        userID := r.Header.Get("X-User-ID")
        ctx := pennant.EvalContext{
            Key: userID,
            Attributes: map[string]any{
                "plan":    r.Header.Get("X-User-Plan"),
                "country": r.Header.Get("CF-IPCountry"),
            },
        }

        if client.BoolVariation("new-checkout", ctx, false) {
            serveNewCheckout(w, r)
        } else {
            serveLegacyCheckout(w, r)
        }
    })

    log.Fatal(http.ListenAndServe(":8080", nil))
}
```
