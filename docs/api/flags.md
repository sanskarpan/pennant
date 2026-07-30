# Flags API

All flag endpoints require JWT Bearer authentication. Replace `$TOKEN` with a valid access token from `POST /auth/login`.

## List flags

```
GET /api/v1/projects/{projectKey}/environments/{envKey}/flags
```

```bash
curl http://localhost:8080/api/v1/projects/default/environments/production/flags \
  -H "Authorization: Bearer $TOKEN"
```

**Response `200 OK`:**

```json
[
  {
    "key": "dark-mode",
    "name": "Dark Mode",
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
    "prerequisites": [],
    "targets": [],
    "createdAt": "2024-01-15T10:00:00Z",
    "updatedAt": "2024-01-15T10:00:00Z",
    "version": 3
  }
]
```

## Create a flag

```
POST /api/v1/projects/{projectKey}/environments/{envKey}/flags
```

**Minimum required fields:** `key`, `type`, `variations`, `defaultVariation`.

```bash
curl -X POST http://localhost:8080/api/v1/projects/default/environments/production/flags \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "key": "new-checkout",
    "name": "New Checkout Flow",
    "type": "boolean",
    "enabled": false,
    "defaultVariation": "off",
    "variations": [
      {"key": "on",  "value": true},
      {"key": "off", "value": false}
    ]
  }'
```

**Response `201 Created`:** Full flag object (same shape as above).

### Flag types

| Type | `value` type | Use case |
|---|---|---|
| `boolean` | `true` / `false` | Simple on/off toggles |
| `string` | any string | A/B text variants, config values |
| `integer` | integer number | Numeric config, limits |
| `json` | any JSON | Complex configs, objects |

## Get a flag

```
GET /api/v1/projects/{projectKey}/environments/{envKey}/flags/{flagKey}
```

```bash
curl http://localhost:8080/api/v1/projects/default/environments/production/flags/new-checkout \
  -H "Authorization: Bearer $TOKEN"
```

## Update a flag

```
PUT /api/v1/projects/{projectKey}/environments/{envKey}/flags/{flagKey}
```

PUT replaces the full flag definition. Include all fields, not just the changed ones.

```bash
curl -X PUT http://localhost:8080/api/v1/projects/default/environments/production/flags/new-checkout \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "New Checkout Flow",
    "type": "boolean",
    "enabled": true,
    "defaultVariation": "off",
    "variations": [
      {"key": "on",  "value": true},
      {"key": "off", "value": false}
    ],
    "targets": [
      {
        "contextKeys": ["internal-qa-1", "internal-qa-2"],
        "variation": "on"
      }
    ],
    "rules": [
      {
        "id": "rule-beta",
        "clauses": [
          {"attribute": "plan", "op": "in", "values": ["beta"]}
        ],
        "variation": "on"
      }
    ],
    "rollout": [
      {"variation": "on",  "weight": 5000},
      {"variation": "off", "weight": 95000}
    ],
    "prerequisites": []
  }'
```

**Response `200 OK`:** Updated flag object.

### Adding prerequisites

```bash
curl -X PUT .../flags/my-feature \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    ...
    "prerequisites": [
      {
        "flagKey": "infrastructure-v2",
        "variation": "on"
      }
    ]
  }'
```

### Percentage rollout only (no rules)

```bash
curl -X PUT .../flags/my-feature \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "enabled": true,
    "defaultVariation": "off",
    "variations": [
      {"key": "on",  "value": true},
      {"key": "off", "value": false}
    ],
    "rollout": [
      {"variation": "on",  "weight": 10000},
      {"variation": "off", "weight": 90000}
    ],
    "rules": [],
    "targets": [],
    "prerequisites": []
  }'
```

## Delete a flag

```
DELETE /api/v1/projects/{projectKey}/environments/{envKey}/flags/{flagKey}
```

```bash
curl -X DELETE http://localhost:8080/api/v1/projects/default/environments/production/flags/new-checkout \
  -H "Authorization: Bearer $TOKEN"
```

**Response `204 No Content`**

## Audit log

Every flag create/update/delete is recorded in the audit log. Retrieve the last 100 events:

```bash
curl "http://localhost:8080/api/v1/projects/default/audit?limit=100" \
  -H "Authorization: Bearer $TOKEN"
```

Maximum `limit` is `500`.

## Flag object schema

| Field | Type | Description |
|---|---|---|
| `key` | string | Unique identifier within project+environment. URL-safe. |
| `name` | string | Human-readable display name |
| `type` | enum | `boolean`, `string`, `integer`, `json` |
| `enabled` | boolean | Master on/off switch |
| `defaultVariation` | string | Variation key to return when no rule matches |
| `variations` | array | List of `{key, value}` pairs |
| `targets` | array | Individual context key overrides (highest priority) |
| `prerequisites` | array | Other flags that must resolve to a specific variation |
| `rules` | array | Ordered list of clause-based rules |
| `rollout` | array | Percentage weights summing to 100000 |
| `bucketBy` | string | Attribute to bucket on (default: context key) |
| `seed` | string | Hash seed override |
| `version` | integer | Monotonically increasing version number |
| `createdAt` | ISO 8601 | Creation timestamp |
| `updatedAt` | ISO 8601 | Last modification timestamp |
