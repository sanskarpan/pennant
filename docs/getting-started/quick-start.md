# Quick Start

This guide gets Pennant running locally and walks you through creating your first feature flag and evaluating it from an SDK.

## Prerequisites

- Docker and Docker Compose (recommended), **or** Go 1.22+ with CGO enabled

## Step 1: Clone and start

```bash
git clone https://github.com/sanskarpan/pennant
cd pennant
cp .env.example .env
```

Open `.env` and set a strong secret (minimum 32 characters):

```bash
PENNANT_JWT_SECRET=replace-this-with-a-long-random-string-in-production
```

Then start:

```bash
docker compose up
```

The server is ready when you see:

```
pennant  | {"level":"info","msg":"server listening","addr":":8080"}
```

Open [http://localhost:8080](http://localhost:8080) and log in with:

- **Email:** `admin@pennant.local`
- **Password:** `admin`

!!! warning "Change the default password"
    The seeded `admin` password is public knowledge. Change it immediately via the dashboard Settings page before exposing Pennant on a network.

## Step 2: Create your first flag

### Via the dashboard

1. In the sidebar, select the **default** project → **production** environment.
2. Click **New Flag**.
3. Set **Key** to `my-first-flag`, **Type** to `boolean`, and toggle **Enabled** on.
4. Click **Save**.

### Via the API

```bash
# 1. Get a JWT
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@pennant.local","password":"admin"}' \
  | jq -r '.accessToken')

# 2. Create a flag
curl -X POST http://localhost:8080/api/v1/projects/default/environments/production/flags \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "key": "my-first-flag",
    "name": "My First Flag",
    "type": "boolean",
    "enabled": true,
    "defaultVariation": "on",
    "variations": [
      {"key": "on",  "value": true},
      {"key": "off", "value": false}
    ]
  }'
```

## Step 3: Evaluate the flag from your app

### Go SDK

```bash
go get pennant/sdk/go/pennant
```

```go
package main

import (
    "context"
    "fmt"
    "log"

    "pennant/sdk/go/pennant"
)

func main() {
    client, err := pennant.NewClient(pennant.Config{
        BaseURL: "http://localhost:8080",
        SDKKey:  "sdk-server-default-prod",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := pennant.EvalContext{Key: "user-123"}
    enabled := client.BoolVariation("my-first-flag", ctx, false)
    fmt.Printf("my-first-flag = %v\n", enabled)
}
```

### TypeScript SDK

```bash
bun add pennant-sdk   # or: npm install pennant-sdk
```

```typescript
import { PennantClient } from "pennant-sdk";

const client = new PennantClient({
  baseUrl: "http://localhost:8080",
  sdkKey: "sdk-server-default-prod",
});

await client.connect();

const enabled = client.boolVariation("my-first-flag", { key: "user-123" }, false);
console.log("my-first-flag =", enabled);
```

## Step 4: Add a targeting rule

Let's enable the flag only for users in the `beta` group.

```bash
curl -X PUT http://localhost:8080/api/v1/projects/default/environments/production/flags/my-first-flag \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "enabled": true,
    "defaultVariation": "off",
    "variations": [
      {"key": "on",  "value": true},
      {"key": "off", "value": false}
    ],
    "rules": [
      {
        "id": "rule-beta",
        "clauses": [
          {
            "attribute": "group",
            "op": "in",
            "values": ["beta"]
          }
        ],
        "variation": "on"
      }
    ]
  }'
```

Now evaluate with a context that has the `group` attribute:

```go
ctx := pennant.EvalContext{
    Key:        "user-123",
    Attributes: map[string]any{"group": "beta"},
}
enabled := client.BoolVariation("my-first-flag", ctx, false)
// enabled == true
```

## Next steps

- [Configuration reference](configuration.md) — all environment variables
- [How evaluation works](../concepts/evaluation.md) — priority order and bucketing
- [A/B testing](../concepts/ab-testing.md) — run experiments and track metrics
- [Go SDK guide](../sdk/go.md) — reconnect behavior, all variation types
- [TypeScript SDK guide](../sdk/typescript.md) — BigInt bucketing, browser vs. server
