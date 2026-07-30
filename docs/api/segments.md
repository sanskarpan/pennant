# Segments API

Segments are scoped to a **project** (not an environment). All segment endpoints require JWT Bearer authentication.

## List segments

```
GET /api/v1/projects/{projectKey}/segments
```

```bash
curl http://localhost:8080/api/v1/projects/default/segments \
  -H "Authorization: Bearer $TOKEN"
```

**Response `200 OK`:**

```json
[
  {
    "key": "beta-testers",
    "name": "Beta Testers",
    "included": ["alice", "bob"],
    "excluded": [],
    "rules": [
      {
        "clauses": [
          {"attribute": "plan", "op": "in", "values": ["beta", "enterprise"]}
        ]
      }
    ],
    "createdAt": "2024-01-10T09:00:00Z",
    "updatedAt": "2024-01-15T14:30:00Z",
    "version": 2
  }
]
```

## Create a segment

```
POST /api/v1/projects/{projectKey}/segments
```

```bash
curl -X POST http://localhost:8080/api/v1/projects/default/segments \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "key": "beta-testers",
    "name": "Beta Testers",
    "included": ["alice", "bob"],
    "excluded": [],
    "rules": [
      {
        "clauses": [
          {
            "attribute": "plan",
            "op": "in",
            "values": ["beta", "enterprise"]
          }
        ]
      }
    ]
  }'
```

**Response `201 Created`:** Full segment object.

### Segment with multiple rules

Rules use OR semantics — a context matching any rule is a member.

```bash
curl -X POST http://localhost:8080/api/v1/projects/default/segments \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "key": "power-users",
    "name": "Power Users",
    "included": [],
    "excluded": [],
    "rules": [
      {
        "clauses": [
          {"attribute": "loginCount", "op": "gt", "values": ["100"]},
          {"attribute": "plan", "op": "in", "values": ["pro", "enterprise"]}
        ]
      },
      {
        "clauses": [
          {"attribute": "role", "op": "in", "values": ["admin", "superuser"]}
        ]
      }
    ]
  }'
```

This segment matches users who:
- Have logged in more than 100 times AND are on `pro`/`enterprise`, **or**
- Have `role` of `admin` or `superuser`

### Segment with semver rule

```bash
curl -X POST http://localhost:8080/api/v1/projects/default/segments \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "key": "new-app-users",
    "name": "Users on app v3+",
    "included": [],
    "excluded": [],
    "rules": [
      {
        "clauses": [
          {"attribute": "appVersion", "op": "semverGt", "values": ["2.9.9"]}
        ]
      }
    ]
  }'
```

## Get a segment

```
GET /api/v1/projects/{projectKey}/segments/{segmentKey}
```

```bash
curl http://localhost:8080/api/v1/projects/default/segments/beta-testers \
  -H "Authorization: Bearer $TOKEN"
```

## Update a segment

```
PUT /api/v1/projects/{projectKey}/segments/{segmentKey}
```

PUT replaces the full segment definition. All connected SDKs receive the update via SSE.

```bash
curl -X PUT http://localhost:8080/api/v1/projects/default/segments/beta-testers \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Beta Testers",
    "included": ["alice", "bob", "charlie"],
    "excluded": ["flaky-test-account"],
    "rules": [
      {
        "clauses": [
          {"attribute": "plan", "op": "in", "values": ["beta", "enterprise"]}
        ]
      }
    ]
  }'
```

**Response `200 OK`:** Updated segment object.

## Delete a segment

```
DELETE /api/v1/projects/{projectKey}/segments/{segmentKey}
```

!!! warning "Cascade effect"
    Deleting a segment does not automatically remove references to it in flag rules. Flags referencing a deleted segment via `segmentMatch` will log a warning during evaluation and treat the clause as not matching. Clean up flag rules before deleting a segment.

```bash
curl -X DELETE http://localhost:8080/api/v1/projects/default/segments/beta-testers \
  -H "Authorization: Bearer $TOKEN"
```

**Response `204 No Content`**

## Using segments in flags

Reference a segment in a flag rule using the `segmentMatch` operator:

```json
{
  "rules": [
    {
      "id": "rule-segment",
      "clauses": [
        {
          "attribute": "",
          "op": "segmentMatch",
          "values": ["beta-testers", "power-users"]
        }
      ],
      "variation": "on"
    }
  ]
}
```

The `values` array is a list of segment keys. The clause matches if the context is a member of **any** of the listed segments.

## Segment object schema

| Field | Type | Description |
|---|---|---|
| `key` | string | Unique identifier within the project. URL-safe. |
| `name` | string | Human-readable display name |
| `included` | string[] | Context keys always in this segment (highest priority) |
| `excluded` | string[] | Context keys never in this segment (overrides included and rules) |
| `rules` | array | List of clause groups (OR semantics between groups, AND within) |
| `version` | integer | Monotonically increasing version number |
| `createdAt` | ISO 8601 | Creation timestamp |
| `updatedAt` | ISO 8601 | Last modification timestamp |
