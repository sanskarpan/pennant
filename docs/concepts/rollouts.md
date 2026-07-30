# Percentage Rollouts

Percentage rollouts let you gradually release a flag variation to a fraction of your user base. Pennant uses a deterministic hash-based bucketing algorithm, so the same user always gets the same variation — the assignment is stable across evaluations, SDK restarts, and server restarts.

## Weight format

Rollout weights are integers in the range `[0, 100000]`, representing percentages with three decimal places of precision:

| Weight | Percentage |
|---|---|
| `0` | 0.000% |
| `100` | 0.100% |
| `1000` | 1.000% |
| `10000` | 10.000% |
| `50000` | 50.000% |
| `100000` | 100.000% |

The weights in a rollout array must sum to exactly `100000`. The server validates this at write time and rejects rollouts that don't sum correctly.

## Example: 10% rollout

```json
{
  "rollout": [
    {"variation": "on",  "weight": 10000},
    {"variation": "off", "weight": 90000}
  ]
}
```

This sends 10% of users to `on` and 90% to `off`.

## The bucketing algorithm

For a given evaluation context, Pennant computes a bucket number using SHA-1:

```
input  = flagKey + "." + bucketByValue
hash   = SHA-1(input)            // 20 bytes
first8 = hash[0:8]               // first 8 bytes (64 bits)
bucket = BigEndian(first8) % BucketScale
```

Where `BucketScale = 1152921504606846975` (= `0x0FFFFFFFFFFFFFFF`, 2^60 - 1).

The `bucket` value is a 64-bit integer in the range `[0, BucketScale)`. To determine which variation a user gets, Pennant walks the rollout array and accumulates weights:

```
threshold = 0
for each entry in rollout:
    threshold += entry.weight * (BucketScale / 100000)
    if bucket < threshold:
        return entry.variation
```

Because the mapping from weight to bucket range is deterministic and uses the user's stable key as input, a user's assignment never changes unless you change the rollout weights or the seed.

## bucketBy attribute

By default, Pennant buckets on `context.Key`. You can override this with the `bucketBy` field to bucket on a different attribute:

```json
{
  "bucketBy": "accountId",
  "rollout": [
    {"variation": "on",  "weight": 20000},
    {"variation": "off", "weight": 80000}
  ]
}
```

This ensures all users sharing the same `accountId` receive the same variation — useful for B2B products where you want consistent rollouts at the company level rather than the individual user level.

If the specified attribute is missing from the context, Pennant falls back to `context.Key`.

## Seed override

Each flag has an optional `seed` field that is mixed into the hash input. Changing the seed re-randomizes all bucket assignments without changing the rollout percentages:

```json
{
  "seed": "experiment-round-2",
  "rollout": [
    {"variation": "treatment", "weight": 50000},
    {"variation": "control",   "weight": 50000}
  ]
}
```

When no seed is set, the hash input is `flagKey + "." + bucketByValue`. When a seed is set, it is `seed + "." + bucketByValue`.

Use seed overrides to:
- Restart an experiment with a fresh user assignment
- Ensure two flags have uncorrelated assignments (give them different seeds)
- Migrate to a new flag key without re-shuffling existing assignments

## Rollout within rules

Rollouts can appear inside rules, not just at the top level. This lets you apply percentage splits only to a specific matched population:

```json
{
  "rules": [
    {
      "id": "rule-us-users",
      "clauses": [
        {"attribute": "country", "op": "in", "values": ["US"]}
      ],
      "rollout": [
        {"variation": "on",  "weight": 50000},
        {"variation": "off", "weight": 50000}
      ]
    }
  ]
}
```

US users get a 50/50 split. Non-US users fall through to the next rule or the top-level rollout.

## Gradual ramp

To gradually ramp a feature from 0% to 100%, update the rollout weights incrementally:

```bash
# Start at 5%
curl -X PUT .../flags/my-flag -d '{"rollout":[{"variation":"on","weight":5000},{"variation":"off","weight":95000}]}'

# Ramp to 25%
curl -X PUT .../flags/my-flag -d '{"rollout":[{"variation":"on","weight":25000},{"variation":"off","weight":75000}]}'

# Full rollout
curl -X PUT .../flags/my-flag -d '{"rollout":[{"variation":"on","weight":100000},{"variation":"off","weight":0}]}'
```

Connected SDK clients receive the weight change via SSE within one heartbeat interval (default 25s) and begin using the new percentages immediately. Users already assigned to `on` at 5% will remain in `on` at 25% (their bucket hasn't changed) — the 20% increase comes from users whose buckets fall in the `[5%, 25%)` range.

## Cross-SDK determinism

The same bucketing algorithm is implemented in both the Go SDK (`internal/eval/bucket.go`) and the TypeScript SDK. Both use BigInt arithmetic to avoid floating-point precision loss when computing `bucket % BucketScale`. The 45+ conformance fixtures in `conformance/` include rollout cases and are run against both implementations on every CI push.
