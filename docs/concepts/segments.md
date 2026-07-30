# Segments and Targeting

Segments are reusable groups of users (or other entities) that can be referenced across multiple flags. Instead of duplicating targeting logic in every flag, you define the group once and reference it by key.

## What is a segment?

A segment is a named collection of matching criteria. A context either matches a segment or it doesn't. Segments are evaluated at flag evaluation time — there is no pre-computation or persistent membership list.

## Segment structure

```json
{
  "key": "beta-testers",
  "name": "Beta Testers",
  "included": ["alice", "bob", "charlie"],
  "excluded": ["dave"],
  "rules": [
    {
      "clauses": [
        {"attribute": "plan", "op": "in", "values": ["beta", "enterprise"]},
        {"attribute": "emailVerified", "op": "in", "values": ["true"]}
      ]
    }
  ]
}
```

## Membership evaluation order

Segment membership is evaluated in this order:

1. **Excluded list:** If `context.Key` is in `excluded`, the context is **not** a member — immediately.
2. **Included list:** If `context.Key` is in `included`, the context **is** a member — immediately.
3. **Rules:** If any rule matches (all clauses in the rule match), the context **is** a member.
4. **Default:** If none of the above apply, the context is **not** a member.

The excluded list takes priority over everything, including the included list. This allows you to add an exception for a specific user even if they would otherwise match a rule.

## Rule semantics

Within a single segment rule, all clauses must match (implicit AND). Multiple rules in the same segment use OR logic — a context matching any rule is a member.

```json
{
  "rules": [
    {
      "clauses": [
        {"attribute": "plan", "op": "in", "values": ["enterprise"]},
        {"attribute": "country", "op": "in", "values": ["US"]}
      ]
    },
    {
      "clauses": [
        {"attribute": "plan", "op": "in", "values": ["beta"]}
      ]
    }
  ]
}
```

This segment matches users who are either:
- On the `enterprise` plan AND in the US, **or**
- On the `beta` plan (anywhere)

## Using segments in flag rules

Reference a segment in a flag rule clause using the `segmentMatch` operator:

```json
{
  "rules": [
    {
      "id": "rule-beta-segment",
      "clauses": [
        {
          "attribute": "",
          "op": "segmentMatch",
          "values": ["beta-testers"]
        }
      ],
      "variation": "on"
    }
  ]
}
```

The `values` array can reference multiple segment keys. The clause matches if the context is a member of **any** of the listed segments.

## Included and excluded lists

The `included` and `excluded` fields are arrays of context keys (string exact-match). They are stored as hash sets internally for O(1) lookup regardless of list size.

```json
{
  "included": ["internal-qa-1", "internal-qa-2", "ceo@example.com"],
  "excluded": ["flaky-test-account"]
}
```

Use `included` for individuals who should always be in the segment regardless of their attributes. Use `excluded` for individuals who should never be in the segment even if they would otherwise match a rule.

## Creating a segment

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
          {"attribute": "plan", "op": "in", "values": ["beta"]}
        ]
      }
    ]
  }'
```

## Updating a segment

Segment updates are propagated to all connected SDKs within one SSE heartbeat interval (default 25 seconds). Flags referencing the updated segment will immediately use the new membership criteria.

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

## Segment scope

Segments are scoped to a **project**, not to an environment. A segment defined in the `default` project is available in all of that project's environments (`production`, `staging`, etc.). Flag rules that reference the segment can be enabled in one environment and disabled in another, but the segment definition itself is shared.

## Performance considerations

- Segment `included`/`excluded` lookups are O(1) hash table operations.
- Segment rules use the same clause evaluation engine as flag rules. Avoid regex clauses (`matches`, `notMatches`) in high-cardinality segments on the hot evaluation path, as regex compilation is cached but matching still has linear cost on the input string.
- Segments are embedded in the flag snapshot delivered to SDKs. The snapshot is computed server-side at write time; SDK evaluation never makes additional network calls to resolve segment membership.
