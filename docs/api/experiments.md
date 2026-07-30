# Experiments API

Experiments link feature flags to metrics. All experiment endpoints require JWT Bearer authentication.

## List experiments

```
GET /api/v1/projects/{projectKey}/experiments
```

```bash
curl http://localhost:8080/api/v1/projects/default/experiments \
  -H "Authorization: Bearer $TOKEN"
```

**Response `200 OK`:**

```json
[
  {
    "key": "checkout-button-color",
    "name": "Checkout Button Color Test",
    "flagKey": "checkout-button-color-flag",
    "metricEventName": "checkout_completed",
    "controlVariation": "control",
    "treatmentVariation": "treatment",
    "status": "running",
    "createdAt": "2024-01-20T08:00:00Z",
    "updatedAt": "2024-01-20T08:00:00Z"
  }
]
```

## Create an experiment

```
POST /api/v1/projects/{projectKey}/experiments
```

**Required:** `key`, `flagKey`, `metricEventName`, `controlVariation`, `treatmentVariation`.

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

**Response `201 Created`:** Full experiment object.

### Prerequisites

Before creating an experiment, ensure:

1. The referenced `flagKey` exists in at least one environment.
2. The flag has a percentage rollout that includes both `controlVariation` and `treatmentVariation`.
3. The `metricEventName` matches the event name your application will send to `POST /sdk/v1/events`.

## Get experiment results

```
GET /api/v1/projects/{projectKey}/experiments/{experimentKey}/results
```

```bash
curl http://localhost:8080/api/v1/projects/default/experiments/checkout-button-color/results \
  -H "Authorization: Bearer $TOKEN"
```

**Response `200 OK`:**

```json
{
  "experimentKey": "checkout-button-color",
  "status": "running",
  "control": {
    "variation": "control",
    "sampleSize": 4821,
    "mean": 0.124,
    "variance": 0.1087,
    "standardError": 0.00474
  },
  "treatment": {
    "variation": "treatment",
    "sampleSize": 4839,
    "mean": 0.151,
    "variance": 0.1281,
    "standardError": 0.00514
  },
  "zTest": {
    "zStatistic": 3.14,
    "pValue": 0.0017,
    "significant": true,
    "alpha": 0.05
  },
  "msprt": {
    "mixtureRatio": 0.031,
    "significant": true,
    "alpha": 0.05
  },
  "srmCheck": {
    "chiSquare": 0.42,
    "pValue": 0.52,
    "srmDetected": false,
    "threshold": 0.001
  },
  "relativeUplift": 0.218,
  "absoluteUplift": 0.027,
  "computedAt": "2024-01-25T12:00:00Z"
}
```

### Interpreting the results

**`zTest.significant: true`** — The observed difference is unlikely to be due to chance at the specified `alpha` level. However, only trust this if you waited for your pre-determined sample size before looking. Use `msprt` if you're peeking at results early.

**`msprt.significant: true`** — The sequential test has accumulated enough evidence to call significance. This is valid to check at any point during the experiment without inflating the false positive rate.

**`srmCheck.srmDetected: true`** — Stop interpreting results immediately. The assignment ratio is significantly different from expected. See [A/B Testing concepts](../concepts/ab-testing.md#srm-detection-sample-ratio-mismatch) for remediation steps.

**`relativeUplift`** — Proportional change: `(treatment.mean - control.mean) / control.mean`. A value of `0.218` means a 21.8% relative improvement.

**`absoluteUplift`** — Raw difference: `treatment.mean - control.mean`. A value of `0.027` means 2.7 percentage points.

### Sample size estimation

Add `?sampleSizeEstimate=true` with MDE parameters to get a pre-experiment sample size estimate:

```bash
curl "http://localhost:8080/api/v1/projects/default/experiments/checkout-button-color/results?sampleSizeEstimate=true&baselineRate=0.12&mde=0.02&alpha=0.05&power=0.80" \
  -H "Authorization: Bearer $TOKEN"
```

| Parameter | Description |
|---|---|
| `baselineRate` | Current conversion rate (0–1) |
| `mde` | Minimum detectable effect (absolute, 0–1) |
| `alpha` | Significance threshold (default `0.05`) |
| `power` | Desired statistical power (default `0.80`) |

## Update an experiment

```
PUT /api/v1/projects/{projectKey}/experiments/{experimentKey}
```

Commonly used to update `status`:

```bash
curl -X PUT http://localhost:8080/api/v1/projects/default/experiments/checkout-button-color \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Checkout Button Color Test",
    "flagKey": "checkout-button-color-flag",
    "metricEventName": "checkout_completed",
    "controlVariation": "control",
    "treatmentVariation": "treatment",
    "status": "concluded"
  }'
```

Valid `status` values: `running`, `paused`, `concluded`.

## Delete an experiment

```
DELETE /api/v1/projects/{projectKey}/experiments/{experimentKey}
```

Deleting an experiment removes it and its accumulated metric data. This action is irreversible.

```bash
curl -X DELETE http://localhost:8080/api/v1/projects/default/experiments/checkout-button-color \
  -H "Authorization: Bearer $TOKEN"
```

**Response `204 No Content`**

## Experiment object schema

| Field | Type | Description |
|---|---|---|
| `key` | string | Unique identifier within the project |
| `name` | string | Human-readable display name |
| `flagKey` | string | Key of the feature flag driving variation assignment |
| `metricEventName` | string | Event name to track (must match `POST /sdk/v1/events` `eventName`) |
| `controlVariation` | string | Variation key for the control group |
| `treatmentVariation` | string | Variation key for the treatment group |
| `status` | enum | `running`, `paused`, `concluded` |
| `createdAt` | ISO 8601 | Creation timestamp |
| `updatedAt` | ISO 8601 | Last modification timestamp |
