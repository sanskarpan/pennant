# How Evaluation Works

Flag evaluation in Pennant is a **pure, deterministic, in-process operation**. The SDK holds an atomic pointer to the current flag snapshot and evaluates each variation call entirely from in-memory data — no network I/O on the hot path.

## Evaluation pipeline

Every `BoolVariation`, `StringVariation`, `IntVariation`, and `JSONVariation` call runs through the same ordered pipeline:

```
Flag disabled?
  └─► return defaultVariation          ← step 1: off check

Prerequisites satisfied?
  └─► if any prerequisite fails,
      return defaultVariation          ← step 2: prerequisites

Individual targets match?
  └─► if context.Key is in a target,
      return that target's variation   ← step 3: individual targets

Rules match?
  └─► evaluate rules in priority order
      └─► first matching rule wins     ← step 4: rules

Percentage rollout
  └─► bucket the context key
      └─► return variation by weight   ← step 5: rollout

Default variation                      ← step 6: fallback
```

### Step 1: Enabled check

If the flag's `enabled` field is `false`, evaluation short-circuits immediately and returns `defaultVariation`. This is the cheapest possible check.

### Step 2: Prerequisites

A flag can declare that another flag must resolve to a specific variation before evaluation continues. Prerequisites are evaluated recursively using the same pipeline. If any prerequisite fails (the prerequisite flag resolves to a different variation than required), the current flag returns `defaultVariation`.

```json
{
  "prerequisites": [
    {
      "flagKey": "payments-v2-backend",
      "variation": "on"
    }
  ]
}
```

This means: "Only evaluate this flag if `payments-v2-backend` resolves to `on` for this context."

!!! warning "Circular prerequisites"
    Pennant detects and rejects circular prerequisite chains at write time. A flag cannot (directly or transitively) depend on itself.

### Step 3: Individual targets

Targets are exact-match rules that bypass all clause evaluation. They are evaluated before rules because they are intended for high-priority overrides (e.g., internal testers, specific customers).

```json
{
  "targets": [
    {
      "contextKeys": ["alice", "bob"],
      "variation": "on"
    }
  ]
}
```

The match is a simple set membership test (`contextKeys` is stored as a hash set internally).

### Step 4: Rules

Rules are evaluated in declared order. Each rule has a list of clauses (conditions) and a variation to return if all clauses match (implicit AND within a rule; OR across rules — first match wins).

A rule can also have a **rollout** instead of a fixed variation, allowing percentage-based allocation within a rule's matched population.

```json
{
  "rules": [
    {
      "id": "rule-beta-users",
      "clauses": [
        {"attribute": "plan", "op": "in", "values": ["beta", "enterprise"]}
      ],
      "variation": "on"
    },
    {
      "id": "rule-country-rollout",
      "clauses": [
        {"attribute": "country", "op": "in", "values": ["US", "CA"]}
      ],
      "rollout": [
        {"variation": "on",  "weight": 20000},
        {"variation": "off", "weight": 80000}
      ]
    }
  ]
}
```

### Step 5: Percentage rollout

If no rule matches, the flag's top-level rollout weights are used. Weights are integers in the range `[0, 100000]` (representing 0.000% to 100.000%) and must sum to exactly `100000`.

See [Percentage Rollouts](rollouts.md) for the full bucketing algorithm.

### Step 6: Default variation

If the rollout is empty or all weights are zero, the flag's `defaultVariation` is returned.

## Clause operators

Pennant supports 14 clause operators:

| Operator | Description | Example |
|---|---|---|
| `in` | Value is in the list | `plan in [beta, enterprise]` |
| `notIn` | Value is not in the list | `country notIn [CN, RU]` |
| `contains` | String contains substring | `email contains @example.com` |
| `notContains` | String does not contain substring | `email notContains test` |
| `startsWith` | String starts with prefix | `userId startsWith svc- ` |
| `endsWith` | String ends with suffix | `email endsWith .edu` |
| `matches` | Value matches regex | `userId matches ^user-\d+$` |
| `notMatches` | Value does not match regex | `version notMatches ^0\.` |
| `gt` | Numeric greater than | `accountAge gt 30` |
| `gte` | Numeric greater than or equal | `score gte 100` |
| `lt` | Numeric less than | `errorRate lt 0.01` |
| `lte` | Numeric less than or equal | `retries lte 3` |
| `semverGt` | Semver greater than | `appVersion semverGt 2.0.0` |
| `semverLte` | Semver less than or equal | `appVersion semverLte 1.9.9` |

Semver operators use the `Masterminds/semver/v3` library and follow standard semver precedence rules.

## Evaluation context

An evaluation context has a required `key` (the stable identifier for the entity being evaluated — typically a user ID or device ID) and optional `attributes`:

```go
// Go
ctx := pennant.EvalContext{
    Key: "user-123",
    Attributes: map[string]any{
        "plan":       "enterprise",
        "country":    "US",
        "appVersion": "2.3.1",
        "score":      145.5,
    },
}
```

```typescript
// TypeScript
const ctx = {
  key: "user-123",
  attributes: {
    plan: "enterprise",
    country: "US",
    appVersion: "2.3.1",
    score: 145.5,
  },
};
```

Attribute values can be strings, numbers, or booleans. Clause operators coerce types where it makes sense (e.g., `gt` converts strings to floats before comparison).

## Atomic snapshot updates

The SDK stores the flag snapshot in an atomic pointer (Go: `atomic.Pointer[Snapshot]`, TypeScript: a reference updated under a lock). When the SSE stream delivers a `patch` event, the SDK:

1. Applies the delta to a copy of the current snapshot
2. Atomically swaps the pointer to the new snapshot

Ongoing `BoolVariation` calls that started before the swap complete against the old snapshot. New calls after the swap use the new snapshot. There is no lock contention on the evaluation hot path.

## Explain mode

The Go SDK exposes `ExplainVariation` which returns the same variation result plus a reason indicating which pipeline step produced it:

```go
variation, reason := client.ExplainBoolVariation("my-flag", ctx, false)
// reason: "rule:rule-beta-users" | "target" | "rollout" | "default" | "off" | "prerequisite"
```

This is useful for debugging unexpected flag evaluations in development.
