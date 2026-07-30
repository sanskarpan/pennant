# A/B Testing

Pennant has a built-in A/B testing engine. Experiments are linked to feature flags — you use a flag's percentage rollout to assign users to treatment/control, then track metric events that the engine uses to compute statistical significance.

## How it works

1. **Create a flag** with a percentage rollout (e.g., 50% `treatment`, 50% `control`).
2. **Create an experiment** linked to that flag, specifying the metric event name to track.
3. **Track events** from your app when users convert (e.g., purchase, click, sign-up).
4. **Check results** — the engine computes z-test statistics, mSPRT sequential test, SRM detection, and sample size estimates.

## Creating an experiment

```bash
curl -X POST http://localhost:8080/api/v1/projects/default/experiments \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "key": "checkout-button-color",
    "name": "Checkout Button Color Test",
    "flagKey": "checkout-button-color-flag",
    "metricEventName": "checkout_completed",
    "controlVariation": "control",
    "treatmentVariation": "treatment"
  }'
```

The experiment will track all `checkout_completed` events and split them by which variation the user was assigned to (as determined by the linked flag's rollout).

## Tracking events

From your application, send events to the SDK events endpoint when a user takes the action you're measuring:

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
        "value": 1
      }
    ]
  }'
```

For revenue experiments, set `value` to the revenue amount. For conversion experiments, use `value: 1`.

### Go SDK

```go
client.Track("checkout_completed", pennant.EvalContext{Key: "user-123"}, 1.0)
```

### TypeScript SDK

```typescript
client.track("checkout_completed", { key: "user-123" }, 1.0);
```

Events are queued in memory and flushed to the database in batches (default: every 1 second, 4 worker goroutines). Flushing is non-blocking — your app never waits for event persistence.

## Reading results

```bash
curl http://localhost:8080/api/v1/projects/default/experiments/checkout-button-color/results \
  -H "Authorization: Bearer $TOKEN"
```

Example response:

```json
{
  "experimentKey": "checkout-button-color",
  "status": "running",
  "control": {
    "variation": "control",
    "sampleSize": 4821,
    "mean": 0.124,
    "variance": 0.1087
  },
  "treatment": {
    "variation": "treatment",
    "sampleSize": 4839,
    "mean": 0.151,
    "variance": 0.1281
  },
  "zTest": {
    "zStatistic": 3.14,
    "pValue": 0.0017,
    "significant": true
  },
  "msprt": {
    "mixtureRatio": 0.031,
    "significant": true
  },
  "srmCheck": {
    "chiSquare": 0.42,
    "pValue": 0.52,
    "srmDetected": false
  },
  "relativeUplift": 0.218,
  "absoluteUplift": 0.027
}
```

## Statistical methods

### z-test (fixed-horizon)

The classical two-sample z-test for difference in proportions (or means). The z-statistic compares the normalized difference between treatment and control means against a standard normal distribution.

**When to use:** At a predetermined sample size, after you've stopped collecting data. The p-value is only valid if you don't peek at results while the experiment is running.

**Significance threshold:** Default `alpha = 0.05` (configurable in `config.yaml` under `experiments.default_alpha`).

### mSPRT (sequential test)

The mixture Sequential Probability Ratio Test uses a Gaussian mixture prior to produce a **valid p-value at any sample size**. Unlike the z-test, you can peek at mSPRT results during data collection without inflating the false positive rate.

The `mixtureRatio` value is the likelihood ratio of the treatment hypothesis to the null. The test is significant when this ratio exceeds `1 / alpha`.

**When to use:** When you want to make decisions as soon as the experiment reaches significance, without waiting for a fixed sample size. Use mSPRT as your primary decision criterion.

### SRM detection (Sample Ratio Mismatch)

The engine runs a chi-square goodness-of-fit test comparing the observed assignment ratio to the expected ratio (from the flag's rollout weights). A significant result (`srmDetected: true`) means the experiment has a data integrity problem.

**Default SRM threshold:** `p < 0.001` (configurable in `config.yaml` under `experiments.srm_threshold`).

**What to do if SRM is detected:**
1. Do **not** interpret treatment/control results — they are not reliable.
2. Check for bot traffic or crawler sessions being assigned to one variation.
3. Check that your event tracking fires on the same population as the flag evaluation (e.g., don't track server-side events for client-side flag assignments).
4. Check for redirect-based assignment bugs where one variation loses users in the redirect.
5. Fix the root cause, reset the experiment (update the flag seed to re-randomize assignments), and re-run.

### Welford online variance

Variance is computed incrementally using Welford's online algorithm. This is numerically stable and avoids the need to store all individual event values — the running mean and M2 (sum of squared deviations) are maintained and updated on each event insert.

## Sample size planning

Before starting an experiment, estimate the required sample size:

```bash
curl "http://localhost:8080/api/v1/projects/default/experiments/checkout-button-color/results?sampleSizeEstimate=true&baselineRate=0.12&mde=0.02&alpha=0.05&power=0.80"
```

Returns the estimated sample size per variation needed to detect a minimum detectable effect (MDE) of 2 percentage points (absolute) at 80% power with alpha=0.05.

## Experiment lifecycle

| Status | Description |
|---|---|
| `running` | Experiment is active, events are being collected |
| `paused` | Event collection paused; results preserved |
| `concluded` | Experiment ended; results are final |

Conclude an experiment by updating the linked flag to 100% on the winning variation (or rolling back to 0%), then marking the experiment as `concluded` in the API.
