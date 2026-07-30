# TypeScript SDK

The TypeScript SDK evaluates feature flags locally using an in-memory snapshot. It runs in Node.js and browser environments and uses the same evaluation algorithm as the Go SDK — cross-SDK parity is enforced by 45+ shared conformance fixtures.

## Installation

```bash
# Bun
bun add pennant-sdk

# npm
npm install pennant-sdk

# pnpm
pnpm add pennant-sdk
```

The TypeScript SDK source is in `sdk/ts/src/` of the Pennant repository.

## Creating a client

```typescript
import { PennantClient } from "pennant-sdk";

const client = new PennantClient({
  baseUrl: "http://localhost:8080",
  sdkKey: "sdk-server-default-prod",
});

await client.connect();
```

`connect()` performs the initial snapshot fetch and begins streaming SSE updates. It resolves when the snapshot is loaded and the client is ready to evaluate flags.

### Config options

| Option | Type | Default | Description |
|---|---|---|---|
| `baseUrl` | string | (required) | Pennant server base URL, no trailing slash |
| `sdkKey` | string | (required) | SDK key for the target environment |
| `fetchOptions` | `RequestInit` | `{}` | Additional options passed to every `fetch()` call |

## Evaluation context

```typescript
const ctx = {
  key: "user-123",          // Required: stable identifier
  attributes: {             // Optional: arbitrary key-value pairs
    plan: "enterprise",
    country: "US",
    appVersion: "2.3.1",
    score: 145.5,
    active: true,
  },
};
```

## Variation methods

### boolVariation

```typescript
const enabled: boolean = client.boolVariation("my-flag", ctx, false);
// Third argument: default value returned if flag not found
```

### stringVariation

```typescript
const theme: string = client.stringVariation("ui-theme", ctx, "default");
```

### intVariation

```typescript
const limit: number = client.intVariation("rate-limit", ctx, 100);
```

### jsonVariation

```typescript
const config: unknown = client.jsonVariation("feature-config", ctx, {});
// Returns the raw JSON value; cast to your expected type
const typed = config as MyConfigType;
```

## Tracking events (for A/B testing)

```typescript
client.track("checkout_completed", ctx, 1);
client.track("revenue", ctx, 49.99);
```

Events are queued and flushed asynchronously. `track()` does not block.

## BigInt bucketing

The TypeScript SDK uses `BigInt` for all bucketing arithmetic. JavaScript's `number` type (64-bit float) cannot represent integers larger than `2^53 - 1` without precision loss. `BucketScale` is `2^60 - 1`, which exceeds that limit — using `number` would produce incorrect bucket assignments for roughly 1 in 128 evaluation contexts.

```typescript
// Internal bucketing logic (simplified)
const hash = sha1(input);                                  // Uint8Array
const first8 = hash.slice(0, 8);
const hashInt = toBigInt64BE(first8);                     // BigInt
const bucket = hashInt % BigInt("1152921504606846975");   // BigInt
```

The conformance fixtures include cases where `number` arithmetic would produce the wrong result. These fixtures verify BigInt correctness in CI.

## SSE streaming and reconnect

The SDK opens an SSE connection to `/sdk/v1/stream` after `connect()`. In browsers, it uses the native `EventSource` API. In Node.js, it uses `fetch` with streaming body parsing.

On disconnect:
1. The SDK sends `Last-Event-ID` with the last received snapshot version.
2. The server replays missed events from its `EventRing`.
3. The SDK applies delta patches or a full snapshot as appropriate.
4. Exponential backoff (1s → 2s → 4s → up to 60s) with jitter prevents thundering herds.

During reconnect, evaluation continues against the last known snapshot.

## Closing the client

```typescript
await client.close();
```

`close()` cancels the SSE connection and flushes pending analytics events. Call it during application teardown (e.g., in a `beforeDestroy` lifecycle hook or process `SIGTERM` handler).

## React integration

Use a context provider to share the client across components:

```typescript
// PennantProvider.tsx
import React, { createContext, useContext, useEffect, useState } from "react";
import { PennantClient } from "pennant-sdk";

const PennantContext = createContext<PennantClient | null>(null);

export function PennantProvider({ children }: { children: React.ReactNode }) {
  const [client, setClient] = useState<PennantClient | null>(null);

  useEffect(() => {
    const c = new PennantClient({
      baseUrl: process.env.NEXT_PUBLIC_PENNANT_URL!,
      sdkKey: process.env.NEXT_PUBLIC_PENNANT_SDK_KEY!,
    });
    c.connect().then(() => setClient(c));
    return () => { c.close(); };
  }, []);

  return (
    <PennantContext.Provider value={client}>
      {children}
    </PennantContext.Provider>
  );
}

// Custom hook
export function useFlag(key: string, defaultValue: boolean): boolean {
  const client = useContext(PennantContext);
  const userKey = getCurrentUserId(); // your auth hook
  if (!client) return defaultValue;
  return client.boolVariation(key, { key: userKey }, defaultValue);
}
```

```typescript
// Usage in a component
function CheckoutButton() {
  const useNewCheckout = useFlag("new-checkout-flow", false);
  return useNewCheckout ? <NewCheckoutButton /> : <LegacyCheckoutButton />;
}
```

!!! note "Re-renders on flag updates"
    The example above does not automatically re-render when flags change via SSE. For reactive updates, subscribe to the client's `onUpdate` callback and call a state setter to trigger a re-render.

## Node.js server usage

In a Node.js server, create the client once at startup and close it on shutdown:

```typescript
import { PennantClient } from "pennant-sdk";

let client: PennantClient;

export async function initPennant() {
  client = new PennantClient({
    baseUrl: process.env.PENNANT_URL!,
    sdkKey: process.env.PENNANT_SDK_KEY!,
  });
  await client.connect();
}

export function getClient(): PennantClient {
  return client;
}

// In your Express/Fastify route handler:
app.get("/", (req, res) => {
  const enabled = getClient().boolVariation(
    "new-feature",
    { key: req.user.id, attributes: { plan: req.user.plan } },
    false
  );
  res.json({ newFeature: enabled });
});

// Graceful shutdown
process.on("SIGTERM", async () => {
  await getClient().close();
  process.exit(0);
});
```

## Conformance with the Go SDK

The evaluation algorithm is identical between the Go and TypeScript SDKs. The conformance suite in `conformance/` contains 45+ fixture directories, each with:

- `input.json` — flag snapshot, evaluation context, and flag key
- `expected.json` — expected variation result and reason

Both SDKs are run against every fixture in CI:

```bash
# Go conformance
go test -run TestConformance ./...

# TypeScript conformance (via Makefile)
make conformance
```

If a fixture passes in Go but fails in TypeScript (or vice versa), the CI build fails. This guarantees that any targeting rule or rollout logic works identically regardless of which SDK your application uses.
