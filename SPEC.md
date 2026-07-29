# SPEC.md — Feature Flag Service with Real-Time Updates

> **Backend:** Go 1.22+ (control plane, SSE streaming edge, evaluation engine, experiment stats)
> **SDKs:** Go + TypeScript, sharing a language-neutral evaluation spec + JSON conformance suite
> **Frontend:** React 18 + TypeScript + Vite + shadcn/ui (Radix) + Tailwind + TanStack Query/Table + dnd-kit + Recharts + Monaco

---

## §1 Language & Stack Decision

### Backend: Go [yes]

The workload has three distinct shapes, and Go is the only mainstream language that handles all three well without ceremony:

| Workload | Requirement | Why Go |
|----------|-------------|--------|
| **SSE fan-out** | 10k+ concurrent long-lived HTTP connections, one goroutine each | ~2–5 KiB per SSE connection; goroutine-per-connection is idiomatic and cheap. `http.Flusher` makes SSE ~20 lines of stdlib code. |
| **Flag evaluation** | Sub-millisecond, allocation-light, deterministic hashing | In-memory rule tree walk, `hash/fnv` + `murmur3`, zero-GC-pressure evaluation paths |
| **Event ingestion + stats** | High-throughput batched writes, then CPU-bound statistics | Buffered channels for ingestion; `math` for Welford/z-test/SPRT without pulling in a numerics stack |

Rust would be faster but 3× slower to build and the SDK story gets worse. Node/TS gives one language end-to-end but is the wrong shape for 10k concurrent connections plus CPU-bound sequential-testing math. **Go is the correct answer.**

### The SDK is a first-class deliverable — in *two* languages

This is the architectural insight that distinguishes this project from a CRUD app. The whole point of a feature flag service is that **evaluation happens locally in the SDK, not on the server**:

> LaunchDarkly's server-side SDKs establish a streaming connection to fetch all flag rules on initialization, caching them in an in-memory store. When application code calls `variation()`, the evaluation is a local, in-memory operation with **no network I/O on the critical path**.

That means the evaluation algorithm must be implemented N times, once per language, and **must produce byte-identical results**. OpenFeature's own docs call this out as the hard part:

> "Considering rules like context-property-based and percentage-rollout strategies via hashing to buckets, it can be hard to keep 100% feature parity across languages."

So this project ships:
1. **`EVALUATION.md`** — a language-neutral normative spec of the evaluation algorithm
2. **`conformance/*.json`** — ~200 shared test fixtures (flag config + context → expected variation + reason)
3. **Go SDK** and **TypeScript SDK**, both running the same fixtures in CI

If the two SDKs ever disagree on a single fixture, the build fails. That is the single most valuable engineering property in the whole system.

### Frontend: React + shadcn/ui + Tailwind (NOT React Flow, NOT vanilla D3)

Each previous project got a different frontend because each had a different UI shape:

| Project | UI shape | Stack |
|---------|----------|-------|
| Vector clocks / LSM / B+tree | Bespoke custom visualizations | Vanilla TS + D3 |
| DAG scheduler | Interactive node graph | React + React Flow + dagre |
| **Feature flags (this)** | **Dense admin console: tables, forms, nested rule builder, charts** | **React + shadcn/ui + TanStack Table + dnd-kit** |

The dominant UI here is a **targeting rule builder** — nested boolean conditions, drag-to-reorder rules, percentage split sliders that must sum to 100. That's a form-and-list problem, not a graph problem:

- **shadcn/ui + Radix** — accessible Select/Combobox/Dialog/Popover primitives. The rule builder needs ~8 of these and hand-rolling them is a waste of a week.
- **react-hook-form + zod** — nested field arrays (rules → clauses) with live validation. Zod schemas are shared with the API types.
- **dnd-kit** — rule reordering. **Rule order is semantically meaningful** (first match wins), so drag-to-reorder is a core feature, not a nicety.
- **TanStack Table** — flag list with filtering, sorting, column visibility, pagination.
- **TanStack Query** — server state + SSE-driven cache invalidation.
- **Recharts** — experiment result charts (conversion over time, confidence intervals).
- **Monaco Editor** — JSON variation values.
- **Vite + Bun** — build/dev.

---

## §2 Concepts Covered

| Area | Concepts |
|------|----------|
| Flag model | Boolean / string / number / JSON variations, multivariate flags, default-off state, archived flags |
| Targeting | Individual targets, rule clauses (14 operators), segments, rule ordering (first-match-wins), prerequisites |
| Rollouts | Deterministic bucketing, percentage rollouts, weighted variations, `bucketBy` custom attribute, salt/seed |
| Real-time delivery | SSE streaming, `Last-Event-ID` replay, heartbeats, reconnect backoff, delta vs full-payload push |
| SDK architecture | Local evaluation, in-memory store, streaming vs polling, initialization, graceful degradation, offline mode |
| Cross-language parity | Normative evaluation spec + JSON conformance fixtures run by every SDK in CI |
| Environments | Project → environment isolation, per-environment flag state, environment cloning, SDK key scoping |
| Config management | Versioned config, atomic snapshot publishing, config checksum, audit log, change diffing |
| Safety | Kill switch, prerequisite flags, scheduled changes, approval workflow, flag dependencies |
| A/B testing | Experiment assignment, exposure events, conversion metrics, two-proportion z-test, confidence intervals |
| Sequential testing | mSPRT / always-valid p-values, peeking problem, early stopping |
| Data quality | Sample ratio mismatch (SRM) detection via chi-square |
| Analytics | Event ingestion, batching, deduplication, flag evaluation counters, stale flag detection |
| Observability | Per-flag eval counts, variation distribution, SDK connection health, propagation latency (p50/p99) |

---

## §3 Architecture

```
                    Admin Console (React + shadcn/ui)
                              │
                      REST + SSE (:8080)
                              │
        ┌─────────────────────┴──────────────────────┐
        │                                            │
        ▼                                            ▼
  Control Plane (Go)                          Streaming Edge (Go)
  ├─ Flag CRUD + validation                   ├─ SSE Hub (fan-out)
  ├─ Rule engine (server-side eval)           ├─ Per-environment channels
  ├─ Segment management                       ├─ Event ID ring buffer (replay)
  ├─ Audit log                                └─ Heartbeat ticker (25s)
  ├─ Config snapshot publisher                          │
  └─ Experiment manager                                 │
        │                                               │
        ▼                                               │
  ConfigStore (SQLite / in-mem)                         │
  ├─ flags, segments, environments                      │
  ├─ config versions (monotonic)                        │
  └─ audit entries                                      │
        │                                               │
        └──────────► Snapshot ──────────────────────────┘
                    (versioned, checksummed)
                                                        │
                            ┌───────────────────────────┴────────────┐
                            ▼                                        ▼
                    Go SDK (in-process)                     TypeScript SDK
                    ├─ SSE client + backoff                 ├─ EventSource + backoff
                    ├─ In-memory flag store                 ├─ In-memory flag store
                    ├─ Local evaluator ◄──── same spec ───► ├─ Local evaluator
                    ├─ Event buffer (batched flush)         ├─ Event buffer
                    └─ variation() — zero network I/O       └─ variation()
                            │                                        │
                            └──────────► Event Ingest API ◄──────────┘
                                              │
                                              ▼
                                     Analytics Pipeline (Go)
                                     ├─ Exposure aggregation
                                     ├─ Conversion aggregation
                                     ├─ Welford running stats
                                     ├─ z-test / mSPRT
                                     └─ SRM chi-square check
```

---

## §4 Data Model

### 4.1 Core entities

```go
// internal/model/flag.go

type VariationType string
const (
    TypeBoolean VariationType = "boolean"
    TypeString  VariationType = "string"
    TypeNumber  VariationType = "number"
    TypeJSON    VariationType = "json"
)

type Variation struct {
    ID          string          `json:"id"`           // stable ID, never reused
    Name        string          `json:"name"`         // "control", "treatment-a"
    Description string          `json:"description"`
    Value       json.RawMessage `json:"value"`        // typed per flag.Type
}

type Flag struct {
    Key            string        `json:"key"`          // "new-checkout-flow"
    Name           string        `json:"name"`
    Description    string        `json:"description"`
    Type           VariationType `json:"type"`
    Variations     []Variation   `json:"variations"`   // >= 2
    Tags           []string      `json:"tags"`
    Temporary      bool          `json:"temporary"`    // flags meant to be removed
    Archived       bool          `json:"archived"`
    CreatedAt      time.Time     `json:"created_at"`
    UpdatedAt      time.Time     `json:"updated_at"`

    // Per-environment configuration
    Environments map[string]*FlagConfig `json:"environments"` // envKey -> config
}

type FlagConfig struct {
    On                bool          `json:"on"`               // master switch (kill switch)
    Prerequisites     []Prerequisite `json:"prerequisites"`
    Targets           []Target      `json:"targets"`          // individual key targeting
    Rules             []Rule        `json:"rules"`            // ORDERED — first match wins
    Fallthrough       VariationOrRollout `json:"fallthrough"` // when on and nothing matched
    OffVariation      *int          `json:"off_variation"`    // index used when on == false
    Salt              string        `json:"salt"`             // bucketing salt; changing it re-buckets everyone
    TrackEvents       bool          `json:"track_events"`     // emit exposure events
    Version           int64         `json:"version"`          // monotonic per flag+env
}

type Prerequisite struct {
    FlagKey   string `json:"flag_key"`
    Variation int    `json:"variation"`   // required variation index of the prereq flag
}

type Target struct {
    Variation   int      `json:"variation"`
    ContextKeys []string `json:"context_keys"`
}

type Rule struct {
    ID          string  `json:"id"`
    Description string  `json:"description"`
    Clauses     []Clause `json:"clauses"`     // ALL must match (AND)
    VariationOrRollout
}

type VariationOrRollout struct {
    Variation *int     `json:"variation,omitempty"`  // fixed variation index
    Rollout   *Rollout `json:"rollout,omitempty"`    // OR percentage rollout
}

type Rollout struct {
    Variations []WeightedVariation `json:"variations"`
    BucketBy   string              `json:"bucket_by"` // attribute name; default "key"
    Seed       *int                `json:"seed"`      // optional explicit bucketing seed
}

type WeightedVariation struct {
    Variation int `json:"variation"`
    Weight    int `json:"weight"`   // 0..100000; all weights sum to 100000
}
```

**Why weights are 0–100000, not 0–100:** three-way even splits (33.333%) are exactly representable, and 0.001% granularity supports canary releases to a tiny slice of very large user bases. This matches LaunchDarkly's model.

### 4.2 Clauses and operators

```go
type Clause struct {
    Attribute string            `json:"attribute"`  // "email", "plan", "country", "key"
    Op        Operator          `json:"op"`
    Values    []json.RawMessage `json:"values"`     // clause matches if ANY value matches (OR)
    Negate    bool              `json:"negate"`
}

type Operator string
const (
    OpIn             Operator = "in"                // exact equality against any value
    OpEndsWith       Operator = "endsWith"
    OpStartsWith     Operator = "startsWith"
    OpMatches        Operator = "matches"           // regex
    OpContains       Operator = "contains"
    OpLessThan       Operator = "lessThan"
    OpLessThanOrEq   Operator = "lessThanOrEqual"
    OpGreaterThan    Operator = "greaterThan"
    OpGreaterThanOrEq Operator = "greaterThanOrEqual"
    OpBefore         Operator = "before"            // datetime
    OpAfter          Operator = "after"             // datetime
    OpSemVerEqual    Operator = "semVerEqual"
    OpSemVerLessThan Operator = "semVerLessThan"
    OpSemVerGreaterThan Operator = "semVerGreaterThan"
    OpSegmentMatch   Operator = "segmentMatch"      // values are segment keys
)
```

**Clause semantics (must match across SDKs):**
- A clause matches if **any** of its `values` matches the context attribute (OR within a clause)
- If the context attribute is an **array**, the clause matches if **any array element** matches any value
- `negate: true` inverts the final clause result
- A missing attribute → clause does not match (before negation)
- A **rule** matches only if **all** its clauses match (AND across clauses)

### 4.3 Segments

```go
type Segment struct {
    Key         string   `json:"key"`
    Name        string   `json:"name"`
    Description string   `json:"description"`
    Included    []string `json:"included"`  // context keys always in segment
    Excluded    []string `json:"excluded"`  // context keys never in segment (wins over Included)
    Rules       []SegmentRule `json:"rules"`
    Salt        string   `json:"salt"`
    Version     int64    `json:"version"`
}

type SegmentRule struct {
    ID       string   `json:"id"`
    Clauses  []Clause `json:"clauses"`
    Weight   *int     `json:"weight,omitempty"`   // 0..100000; optional % of matching contexts
    BucketBy string   `json:"bucket_by"`
}
```

### 4.4 Evaluation context

Modeled on the OpenFeature evaluation context spec.

```go
type Context struct {
    Kind       string                 `json:"kind"`        // "user", "device", "organization"
    Key        string                 `json:"key"`         // targeting key — REQUIRED
    Anonymous  bool                   `json:"anonymous"`
    Attributes map[string]any         `json:"attributes"`  // string|number|bool|datetime|array|object
    Private    []string               `json:"private"`     // attribute names never sent in events
}

func (c *Context) GetAttribute(name string) (any, bool) {
    switch name {
    case "key":       return c.Key, true
    case "kind":      return c.Kind, true
    case "anonymous": return c.Anonymous, true
    }
    v, ok := c.Attributes[name]
    return v, ok
}
```

---

## §5 Evaluation Algorithm (NORMATIVE)

**This section is the contract every SDK must implement identically.** It is reproduced verbatim in `EVALUATION.md` and enforced by `conformance/*.json`.

```
evaluate(flag, context, store) -> (variationIndex, value, reason)

STEP 0 — FLAG NOT FOUND
  If the flag does not exist in the store:
    return (nil, defaultValue, {kind: ERROR, errorKind: FLAG_NOT_FOUND})

STEP 1 — OFF CHECK
  If flag.On == false:
    if flag.OffVariation == nil:
      return (nil, defaultValue, {kind: OFF})
    return (flag.OffVariation, variations[OffVariation].Value, {kind: OFF})

STEP 2 — PREREQUISITES
  For each prerequisite p in flag.Prerequisites (in order):
    prereqFlag = store.GetFlag(p.FlagKey)
    if prereqFlag == nil:
      return offResult(flag, {kind: PREREQUISITE_FAILED, prerequisiteKey: p.FlagKey})
    (prereqIdx, _, _) = evaluate(prereqFlag, context, store)   // RECURSIVE
    if prereqFlag.On == false OR prereqIdx != p.Variation:
      return offResult(flag, {kind: PREREQUISITE_FAILED, prerequisiteKey: p.FlagKey})
  // Cycle guard: a prerequisite chain deeper than MAX_PREREQ_DEPTH (=20)
  // or containing a repeated flag key is an ERROR, not an infinite loop.

STEP 3 — INDIVIDUAL TARGETS
  For each target t in flag.Targets (in order):
    if context.Key is in t.ContextKeys:
      return (t.Variation, variations[t.Variation].Value, {kind: TARGET_MATCH})

STEP 4 — RULES (ORDER MATTERS — FIRST MATCH WINS)
  For i, rule in flag.Rules:
    if ruleMatches(rule, context, store):
      return resolveVariationOrRollout(rule, flag, context,
                                       {kind: RULE_MATCH, ruleIndex: i, ruleId: rule.ID})

STEP 5 — FALLTHROUGH
  return resolveVariationOrRollout(flag.Fallthrough, flag, context, {kind: FALLTHROUGH})


ruleMatches(rule, context, store) -> bool
  For each clause c in rule.Clauses:            // AND across clauses
    if not clauseMatches(c, context, store): return false
  return true


clauseMatches(clause, context, store) -> bool
  if clause.Op == segmentMatch:
    for each segKey in clause.Values:
      if segmentMatches(store.GetSegment(segKey), context, store):
        return clause.Negate ? false : true
    return clause.Negate ? true : false

  attrValue, found = context.GetAttribute(clause.Attribute)
  if not found: return false                    // NOTE: negate does NOT apply to missing attrs

  if attrValue is an array:
    for each element e in attrValue:            // OR across array elements
      for each v in clause.Values:              // OR across clause values
        if matchOperator(clause.Op, e, v): return !clause.Negate
    return clause.Negate
  else:
    for each v in clause.Values:
      if matchOperator(clause.Op, attrValue, v): return !clause.Negate
    return clause.Negate


segmentMatches(segment, context, store) -> bool
  if context.Key in segment.Excluded: return false      // exclusion wins
  if context.Key in segment.Included: return true
  for each rule in segment.Rules:
    allClausesMatch = true
    for each clause in rule.Clauses:
      if not clauseMatches(clause, context, store): allClausesMatch = false; break
    if allClausesMatch:
      if rule.Weight == nil: return true
      bucket = computeBucket(context, rule.BucketBy, segment.Key, segment.Salt, nil)
      return bucket < (rule.Weight / 100000.0)
  return false


resolveVariationOrRollout(vr, flag, context, reason) -> (idx, value, reason)
  if vr.Variation != nil:
    return (vr.Variation, variations[vr.Variation].Value, reason)

  rollout = vr.Rollout
  bucketBy = rollout.BucketBy or "key"
  bucket = computeBucket(context, bucketBy, flag.Key, flag.Salt, rollout.Seed)

  sum = 0.0
  for each wv in rollout.Variations:            // ORDER AS STORED — do not sort
    sum += wv.Weight / 100000.0
    if bucket < sum:
      reason.inExperiment = rollout.IsExperiment
      return (wv.Variation, variations[wv.Variation].Value, reason)

  // Floating-point safety net: if bucket == 1.0 exactly due to rounding,
  // fall through to the LAST weighted variation. Never return an error here.
  last = rollout.Variations[len-1]
  return (last.Variation, variations[last.Variation].Value, reason)
```

### 5.1 Bucketing (the single most important function)

```go
// internal/eval/bucket.go

const BucketScale = 0x7FFFFFFFFFFFFFF // 2^59 - 1; matches LaunchDarkly's constant

// ComputeBucket returns a deterministic float in [0.0, 1.0) for this context.
//
// PROPERTIES THAT MUST HOLD:
//  1. Deterministic: same inputs always produce the same output, in every language,
//     on every machine, forever.
//  2. Uniform: over a large population, buckets are uniformly distributed in [0,1).
//  3. Independent per flag: the same user gets uncorrelated buckets for different flags
//     (otherwise a user unlucky in one 10% rollout is unlucky in ALL of them).
//  4. Monotone under growth: increasing a rollout from 10% -> 20% only ADDS users;
//     nobody who had the feature loses it. This falls out of using a stable bucket
//     value compared against a threshold.
func ComputeBucket(ctx *Context, bucketBy, key, salt string, seed *int) float64 {
    idValue, ok := ctx.GetAttribute(bucketBy)
    if !ok { return 0.0 }

    idStr, ok := stringifyBucketValue(idValue)  // strings and ints only; NOT floats/bools
    if !ok { return 0.0 }

    var hashInput string
    if seed != nil {
        hashInput = fmt.Sprintf("%d.%s", *seed, idStr)
    } else {
        hashInput = fmt.Sprintf("%s.%s.%s", key, salt, idStr)
    }

    sum := sha1.Sum([]byte(hashInput))
    hexPrefix := hex.EncodeToString(sum[:])[:15]   // first 15 hex chars = 60 bits
    n, err := strconv.ParseInt(hexPrefix, 16, 64)
    if err != nil { return 0.0 }

    return float64(n) / float64(BucketScale)
}

// stringifyBucketValue: ONLY strings and integers are valid bucketing values.
// Floats and booleans are rejected because their string representation differs
// across languages (Go "1" vs JS "1", Go "1.5" vs JS "1.5" vs Python "1.5" —
// but Go "1e+10" vs JS "10000000000"). This is a real cross-language parity trap.
func stringifyBucketValue(v any) (string, bool) {
    switch t := v.(type) {
    case string:
        return t, true
    case int:    return strconv.Itoa(t), true
    case int64:  return strconv.FormatInt(t, 10), true
    case float64:
        if t == math.Trunc(t) && math.Abs(t) < 1e15 {
            return strconv.FormatInt(int64(t), 10), true  // integral float is OK
        }
        return "", false                                   // non-integral float is NOT
    default:
        return "", false
    }
}
```

**Why SHA-1 and not MurmurHash:** SHA-1 is in every language's standard library with identical output. MurmurHash3 has multiple incompatible variants (x86_32 vs x64_128) and seed conventions, and getting the TypeScript implementation to match Go bit-for-bit is a known source of parity bugs. SHA-1 is slower (~1µs vs ~50ns) but evaluation is still well under the 1ms budget, and **correctness across languages beats speed here**.

### 5.2 Evaluation reasons

```go
type ReasonKind string
const (
    ReasonOff                ReasonKind = "OFF"
    ReasonFallthrough        ReasonKind = "FALLTHROUGH"
    ReasonTargetMatch        ReasonKind = "TARGET_MATCH"
    ReasonRuleMatch          ReasonKind = "RULE_MATCH"
    ReasonPrerequisiteFailed ReasonKind = "PREREQUISITE_FAILED"
    ReasonError              ReasonKind = "ERROR"
)

type ErrorKind string
const (
    ErrFlagNotFound     ErrorKind = "FLAG_NOT_FOUND"
    ErrMalformedFlag    ErrorKind = "MALFORMED_FLAG"
    ErrWrongType        ErrorKind = "WRONG_TYPE"
    ErrClientNotReady   ErrorKind = "CLIENT_NOT_READY"
    ErrPrereqCycle      ErrorKind = "PREREQUISITE_CYCLE"
)

type Reason struct {
    Kind            ReasonKind `json:"kind"`
    RuleIndex       *int       `json:"rule_index,omitempty"`
    RuleID          string     `json:"rule_id,omitempty"`
    PrerequisiteKey string     `json:"prerequisite_key,omitempty"`
    ErrorKind       ErrorKind  `json:"error_kind,omitempty"`
    InExperiment    bool       `json:"in_experiment,omitempty"`
}
```

The reason is surfaced in the admin console's **Evaluation Debugger** — enter a context, see exactly which rule matched and why.

---

## §6 Config Snapshots & Versioning

The server never streams "flag X changed field Y". It streams **immutable, versioned, checksummed snapshots** (or deltas against a known version). This eliminates an entire class of partial-update bugs.

```go
type Snapshot struct {
    EnvironmentKey string              `json:"environment_key"`
    Version        int64               `json:"version"`      // monotonic per environment
    Flags          map[string]*FlagConfigResolved `json:"flags"`
    Segments       map[string]*Segment `json:"segments"`
    Checksum       string              `json:"checksum"`     // SHA-256 of canonical JSON
    PublishedAt    time.Time           `json:"published_at"`
}

// Delta: sent when the client's Last-Event-ID is recent enough
type Delta struct {
    EnvironmentKey string   `json:"environment_key"`
    FromVersion    int64    `json:"from_version"`
    ToVersion      int64    `json:"to_version"`
    UpsertedFlags  []*FlagConfigResolved `json:"upserted_flags"`
    DeletedFlags   []string `json:"deleted_flags"`
    UpsertedSegments []*Segment `json:"upserted_segments"`
    DeletedSegments  []string `json:"deleted_segments"`
    Checksum       string    `json:"checksum"`  // of the RESULTING snapshot
}
```

**Checksum protocol:** the SDK computes the checksum of its post-apply state and compares. On mismatch it discards local state and requests a full snapshot. This makes delta application self-healing — a bug in delta logic degrades to "slightly more bandwidth", never to "wrong flag values in production".

Canonical JSON for checksumming: keys sorted, no insignificant whitespace, numbers in shortest round-trip form. **Both the Go server and the TS SDK must produce the same canonical bytes** — this is in the conformance suite.

---

## §7 Real-Time Distribution (SSE)

### 7.1 Why SSE, not WebSocket

| Criterion | SSE | WebSocket |
|-----------|-----|-----------|
| Direction needed | Server→client only [yes] | Bidirectional (unused) |
| Per-connection memory | ~2–5 KiB | ~50 KiB |
| Reconnect + replay | Built into `EventSource` via `Last-Event-ID` | Hand-rolled |
| Load balancers / proxies / CDNs | Plain HTTP, works everywhere | Needs upgrade support, sticky sessions |
| Horizontal scaling | Stateless; any instance can serve a reconnect | Needs connection-aware routing |

Flag delivery is a pure fan-out problem, so SSE is strictly the better tool. (LaunchDarkly uses SSE for exactly this reason.)

### 7.2 Wire protocol

```
GET /sdk/stream/{environmentKey}
Authorization: Bearer <sdk-key>
Accept: text/event-stream
Last-Event-ID: 4821          ← optional; server sends a delta if it can

HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no        ← disables nginx response buffering (critical!)

event: put
id: 4821
data: {"environment_key":"production","version":4821,"flags":{...},"segments":{...},"checksum":"sha256:..."}

: heartbeat                  ← SSE comment; keeps proxies from closing idle conns

event: patch
id: 4822
data: {"from_version":4821,"to_version":4822,"upserted_flags":[...],"checksum":"sha256:..."}

event: delete
id: 4823
data: {"from_version":4822,"to_version":4823,"deleted_flags":["old-flag"],"checksum":"sha256:..."}
```

- `put` — full snapshot (initial connect, or replay window exceeded)
- `patch` — delta from `from_version` to `to_version`
- `delete` — flag/segment removals (could fold into `patch`, kept separate for clarity)
- `: heartbeat` — every 25 s (under the typical 30 s proxy idle timeout)

### 7.3 SSE Hub

```go
// internal/stream/hub.go

type Subscriber struct {
    ID       string
    EnvKey   string
    SDKKey   string
    Ch       chan Message     // buffered, capacity 32
    LastSent int64            // last version delivered
    ConnectedAt time.Time
    UserAgent   string
    SDKVersion  string
}

type Hub struct {
    mu     sync.RWMutex
    byEnv  map[string]map[string]*Subscriber  // envKey -> subID -> sub
    ring   map[string]*EventRing              // envKey -> replay buffer
    bus    *events.Bus
    metrics *Metrics
}

// Publish fans out to every subscriber of an environment.
// MUST be non-blocking: a single slow client must never stall the publisher.
func (h *Hub) Publish(envKey string, msg Message) {
    h.mu.RLock()
    subs := h.byEnv[envKey]
    h.mu.RUnlock()

    h.ring[envKey].Append(msg)   // for Last-Event-ID replay

    var slow []string
    for id, s := range subs {
        select {
        case s.Ch <- msg:
            s.LastSent = msg.Version
        default:
            // Buffer full: this client is too slow. Mark for disconnect —
            // it will reconnect and get a full snapshot. That is strictly
            // better than blocking every other subscriber.
            slow = append(slow, id)
        }
    }
    for _, id := range slow {
        h.metrics.SlowClientDisconnects.Inc()
        h.Unsubscribe(envKey, id)
    }
}
```

### 7.4 Replay ring buffer

```go
// EventRing keeps the last N deltas per environment so a client that
// reconnects with Last-Event-ID can be brought up to date cheaply.
type EventRing struct {
    mu       sync.RWMutex
    buf      []Message
    capacity int          // default 256
    head     int
    minVer   int64
    maxVer   int64
}

// Since(version) returns deltas after `version`, or ok=false if the
// requested version has already been evicted (client must take a full snapshot).
func (r *EventRing) Since(version int64) ([]Message, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    if version < r.minVer { return nil, false }  // too old — full snapshot required
    if version >= r.maxVer { return nil, true }  // already current
    var out []Message
    for _, m := range r.ordered() {
        if m.Version > version { out = append(out, m) }
    }
    return out, true
}
```

### 7.5 SSE handler

```go
func (s *Server) HandleStream(w http.ResponseWriter, r *http.Request) {
    envKey := chi.URLParam(r, "envKey")
    sdkKey := extractBearer(r)
    if !s.auth.ValidateSDKKey(sdkKey, envKey) {
        http.Error(w, "unauthorized", http.StatusUnauthorized); return
    }

    flusher, ok := w.(http.Flusher)
    if !ok { http.Error(w, "streaming unsupported", 500); return }

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("X-Accel-Buffering", "no")
    w.WriteHeader(http.StatusOK)
    flusher.Flush()

    sub := s.hub.Subscribe(envKey, sdkKey, r.UserAgent())
    defer s.hub.Unsubscribe(envKey, sub.ID)

    // Initial payload: delta if we can, full snapshot otherwise
    lastID := parseLastEventID(r.Header.Get("Last-Event-ID"))
    if deltas, ok := s.hub.Replay(envKey, lastID); ok && lastID > 0 {
        for _, d := range deltas { writeSSE(w, flusher, d) }
    } else {
        snap := s.store.CurrentSnapshot(envKey)
        writeSSE(w, flusher, Message{Event: "put", ID: snap.Version, Data: snap})
    }

    heartbeat := time.NewTicker(25 * time.Second)
    defer heartbeat.Stop()

    for {
        select {
        case <-r.Context().Done():
            return
        case msg := <-sub.Ch:
            writeSSE(w, flusher, msg)
        case <-heartbeat.C:
            fmt.Fprint(w, ": heartbeat\n\n")
            flusher.Flush()
        }
    }
}

func writeSSE(w io.Writer, f http.Flusher, m Message) {
    data, _ := json.Marshal(m.Data)
    fmt.Fprintf(w, "event: %s\nid: %d\ndata: %s\n\n", m.Event, m.ID, data)
    f.Flush()
}
```

---

## §8 SDK Design

Both SDKs implement the same lifecycle. Differences are language-idiomatic only.

### 8.1 Go SDK

```go
// sdk/go/client.go

type Config struct {
    SDKKey         string
    BaseURL        string
    StreamURL      string
    EventsURL      string
    Mode           Mode          // Streaming | Polling | Offline
    PollInterval   time.Duration // default 30s (polling mode only)
    InitTimeout    time.Duration // default 5s
    EventsCapacity int           // default 10000
    FlushInterval  time.Duration // default 5s
    AllAttributesPrivate bool
    PrivateAttributes    []string
    Logger         Logger
}

type Client struct {
    cfg       Config
    store     *atomic.Pointer[Snapshot]  // lock-free reads on the hot path
    stream    *StreamProcessor
    events    *EventProcessor
    ready     chan struct{}
    readyOnce sync.Once
    status    atomic.Value // ConnectionStatus
}

func NewClient(cfg Config) (*Client, error)

// WaitForInit blocks until the first snapshot arrives or timeout elapses.
// On timeout the client is USABLE but degraded — variation() returns defaults
// with reason CLIENT_NOT_READY. It never returns a hard error, because a flag
// service outage must NEVER take down the calling application.
func (c *Client) WaitForInit(timeout time.Duration) error

func (c *Client) BoolVariation(key string, ctx *Context, def bool) bool
func (c *Client) StringVariation(key string, ctx *Context, def string) string
func (c *Client) IntVariation(key string, ctx *Context, def int) int
func (c *Client) Float64Variation(key string, ctx *Context, def float64) float64
func (c *Client) JSONVariation(key string, ctx *Context, def json.RawMessage) json.RawMessage

// *Detail variants also return the evaluation Reason
func (c *Client) BoolVariationDetail(key string, ctx *Context, def bool) (bool, Reason)

// AllFlags: bootstrap payload for client-side/browser SDKs
func (c *Client) AllFlags(ctx *Context) map[string]any

// Track: record a custom conversion event for experiments
func (c *Client) Track(eventKey string, ctx *Context, value *float64, data map[string]any)

func (c *Client) Flush()
func (c *Client) Close() error

// The hot path: no locks, no allocation beyond the reason struct, no network I/O.
func (c *Client) BoolVariation(key string, ctx *Context, def bool) bool {
    snap := c.store.Load()
    if snap == nil { return def }   // not ready — return default, never block
    flag, ok := snap.Flags[key]
    if !ok {
        c.events.RecordEval(key, ctx, nil, def, Reason{Kind: ReasonError, ErrorKind: ErrFlagNotFound})
        return def
    }
    idx, val, reason := eval.Evaluate(flag, ctx, snap)
    b, ok := val.(bool)
    if !ok {
        return def   // type mismatch: WRONG_TYPE
    }
    if flag.TrackEvents { c.events.RecordEval(key, ctx, idx, b, reason) }
    return b
}
```

### 8.2 TypeScript SDK

```typescript
// sdk/ts/src/client.ts

export interface ClientConfig {
  sdkKey: string;
  baseUrl?: string;
  streamUrl?: string;
  eventsUrl?: string;
  mode?: 'streaming' | 'polling' | 'offline';
  pollIntervalMs?: number;
  initTimeoutMs?: number;
  eventsCapacity?: number;
  flushIntervalMs?: number;
  allAttributesPrivate?: boolean;
  privateAttributes?: string[];
  bootstrap?: Snapshot;   // SSR: hydrate without waiting for the stream
}

export class FeatureFlagClient {
  private snapshot: Snapshot | null = null;
  private es: EventSource | null = null;
  private backoff = new ExponentialBackoff({ baseMs: 1000, maxMs: 30_000, jitter: true });

  async waitForInit(timeoutMs = 5000): Promise<void>;

  boolVariation(key: string, ctx: EvaluationContext, def: boolean): boolean;
  stringVariation(key: string, ctx: EvaluationContext, def: string): string;
  numberVariation(key: string, ctx: EvaluationContext, def: number): number;
  jsonVariation<T>(key: string, ctx: EvaluationContext, def: T): T;
  boolVariationDetail(key: string, ctx: EvaluationContext, def: boolean): EvaluationDetail<boolean>;

  allFlags(ctx: EvaluationContext): Record<string, unknown>;
  track(eventKey: string, ctx: EvaluationContext, value?: number, data?: Record<string, unknown>): void;

  on(event: 'ready' | 'update' | 'error' | 'reconnecting', handler: (payload: unknown) => void): void;

  flush(): Promise<void>;
  close(): void;
}
```

### 8.3 Reconnection with exponential backoff + jitter

```typescript
class ExponentialBackoff {
  private attempt = 0;
  constructor(private opts: { baseMs: number; maxMs: number; jitter: boolean }) {}

  next(): number {
    const exp = Math.min(this.opts.baseMs * 2 ** this.attempt, this.opts.maxMs);
    this.attempt++;
    // Full jitter: prevents a reconnect storm when the server restarts and
    // 10,000 SDKs all try to reconnect at the same millisecond.
    return this.opts.jitter ? Math.random() * exp : exp;
  }
  reset() { this.attempt = 0; }
}
```

### 8.4 Graceful degradation ladder

```
1. Streaming connected           → live updates, sub-second propagation
2. Stream dropped, reconnecting  → serve last known snapshot (STALE); flags keep working
3. Never connected, has cache    → serve persisted snapshot from disk/localStorage
4. Never connected, no cache     → return caller-supplied defaults, reason CLIENT_NOT_READY
```

**A feature flag service outage must never take down the applications that depend on it.** Every layer returns *something*; nothing throws.

---

## §9 Cross-Language Conformance Suite

The highest-value artifact in the project.

```json
// conformance/rollout/weighted_three_way.json
{
  "name": "three-way rollout distributes deterministically",
  "flag": {
    "key": "checkout-experiment",
    "type": "string",
    "salt": "a1b2c3",
    "variations": [
      {"id": "v0", "name": "control",   "value": "control"},
      {"id": "v1", "name": "variant-a", "value": "variant-a"},
      {"id": "v2", "name": "variant-b", "value": "variant-b"}
    ],
    "environments": {
      "production": {
        "on": true,
        "off_variation": 0,
        "fallthrough": {
          "rollout": {
            "bucket_by": "key",
            "variations": [
              {"variation": 0, "weight": 33334},
              {"variation": 1, "weight": 33333},
              {"variation": 2, "weight": 33333}
            ]
          }
        }
      }
    }
  },
  "cases": [
    {"context": {"kind":"user","key":"user-0001"}, "expect": {"variation": 2, "value": "variant-b", "reason": {"kind": "FALLTHROUGH"}}},
    {"context": {"kind":"user","key":"user-0002"}, "expect": {"variation": 0, "value": "control",   "reason": {"kind": "FALLTHROUGH"}}},
    {"context": {"kind":"user","key":"user-0003"}, "expect": {"variation": 1, "value": "variant-a", "reason": {"kind": "FALLTHROUGH"}}}
  ]
}
```

**Fixture categories (~200 total):**

| Category | Count | Covers |
|----------|-------|--------|
| `off/` | 8 | off with/without offVariation, archived flags |
| `targets/` | 12 | individual targeting, target precedence over rules |
| `clauses/` | 60 | all 14 operators × string/number/date/semver/array attributes, negation, missing attributes |
| `rules/` | 25 | rule ordering, multi-clause AND, first-match-wins, rule → rollout |
| `rollout/` | 30 | weight distribution, bucketBy custom attribute, seed, integral-float keys, 0-weight variations |
| `segments/` | 25 | included/excluded precedence, segment rules, weighted segments, segment in clause |
| `prerequisites/` | 15 | pass, fail, prereq off, nested chains, cycle detection |
| `bucketing/` | 20 | exact bucket values for known inputs (locks the hash function forever) |
| `canonical_json/` | 5 | checksum stability across languages |

**CI gate:**
```bash
# .github/workflows/conformance.yml
go test ./sdk/go/... -run TestConformance      # runs all fixtures
cd sdk/ts && bun test conformance.test.ts       # runs the SAME fixtures
# If either fails, or if they disagree on any fixture, the build fails.
```

The `bucketing/` fixtures are especially important: they contain hardcoded expected bucket values like `{"input": "checkout-experiment.a1b2c3.user-0001", "expect_bucket": 0.7834829...}`. Once merged, **the hash function can never be changed** without re-bucketing every user in every deployment — which the fixtures make impossible to do accidentally.

---

## §10 Analytics & Event Ingestion

### 10.1 Event types

```go
type EventKind string
const (
    EventEval      EventKind = "eval"     // a flag was evaluated
    EventTrack     EventKind = "track"    // custom conversion event
    EventIdentify  EventKind = "identify" // context attributes updated
    EventSummary   EventKind = "summary"  // aggregated eval counters
)

type EvalEvent struct {
    Kind         EventKind `json:"kind"`
    CreationDate int64     `json:"creation_date"` // Unix ms
    Key          string    `json:"key"`           // flag key
    ContextKey   string    `json:"context_key"`
    ContextKind  string    `json:"context_kind"`
    Variation    *int      `json:"variation"`
    Value        any       `json:"value"`
    Default      any       `json:"default"`
    Version      int64     `json:"version"`
    Reason       *Reason   `json:"reason,omitempty"`
    InExperiment bool      `json:"in_experiment"`
}

type TrackEvent struct {
    Kind         EventKind      `json:"kind"`
    CreationDate int64          `json:"creation_date"`
    Key          string         `json:"key"`         // metric key, e.g. "checkout-completed"
    ContextKey   string         `json:"context_key"`
    Value        *float64       `json:"value,omitempty"` // revenue, latency, etc.
    Data         map[string]any `json:"data,omitempty"`
}

// SummaryEvent: SDKs aggregate high-volume eval events locally and ship
// counters instead of individual records. A flag evaluated 1M times in a
// 5s window becomes ONE summary event, not 1M eval events.
type SummaryEvent struct {
    Kind      EventKind `json:"kind"`
    StartDate int64     `json:"start_date"`
    EndDate   int64     `json:"end_date"`
    Features  map[string]FlagSummary `json:"features"`
}

type FlagSummary struct {
    Default  any                   `json:"default"`
    Counters []VariationCounter    `json:"counters"`
    ContextKinds []string          `json:"context_kinds"`
}

type VariationCounter struct {
    Variation *int  `json:"variation"`
    Version   int64 `json:"version"`
    Value     any   `json:"value"`
    Count     int   `json:"count"`
    Unknown   bool  `json:"unknown"` // flag not found
}
```

**Why summary events matter:** at LaunchDarkly's reported 45 trillion evaluations/day, shipping one event per evaluation is physically impossible. The SDK keeps an in-memory counter map, flushes every 5s, and only *experiment* evaluations (`inExperiment: true`) are sent as individual records — because experiments need per-user granularity for the join with conversion events.

### 10.2 Ingestion pipeline

```go
type Ingestor struct {
    in       chan []Event          // buffered, capacity 4096
    dedupe   *cuckoo.Filter        // (contextKey, flagKey, variation, minute) dedupe
    store    analytics.Store
    workers  int
}

func (i *Ingestor) Run(ctx context.Context) error {
    g, ctx := errgroup.WithContext(ctx)
    for w := 0; w < i.workers; w++ {
        g.Go(func() error {
            batch := make([]Event, 0, 512)
            tick := time.NewTicker(1 * time.Second)
            defer tick.Stop()
            for {
                select {
                case <-ctx.Done(): return i.store.WriteBatch(batch)
                case evts := <-i.in:
                    batch = append(batch, evts...)
                    if len(batch) >= 512 {
                        i.store.WriteBatch(batch); batch = batch[:0]
                    }
                case <-tick.C:
                    if len(batch) > 0 { i.store.WriteBatch(batch); batch = batch[:0] }
                }
            }
        })
    }
    return g.Wait()
}
```

---

## §11 A/B Testing & Experimentation

### 11.1 Experiment model

```go
type Experiment struct {
    Key            string       `json:"key"`
    Name           string       `json:"name"`
    Hypothesis     string       `json:"hypothesis"`
    FlagKey        string       `json:"flag_key"`
    EnvironmentKey string       `json:"environment_key"`
    Status         ExpStatus    `json:"status"`  // draft|running|stopped|concluded

    ControlVariation int        `json:"control_variation"`
    Treatments       []int      `json:"treatments"`
    TrafficAllocation int       `json:"traffic_allocation"` // 0..100000: % of eligible users in the experiment

    PrimaryMetric  Metric       `json:"primary_metric"`
    SecondaryMetrics []Metric   `json:"secondary_metrics"`
    GuardrailMetrics []Metric   `json:"guardrail_metrics"`

    MinimumDetectableEffect float64 `json:"mde"`       // e.g. 0.02 = 2 percentage points
    Alpha                   float64 `json:"alpha"`     // default 0.05
    Power                   float64 `json:"power"`     // default 0.80
    RequiredSampleSize      int     `json:"required_sample_size"` // computed from MDE/alpha/power

    AnalysisMethod AnalysisMethod `json:"analysis_method"` // fixed_horizon | sequential
    StartedAt      *time.Time  `json:"started_at"`
    StoppedAt      *time.Time  `json:"stopped_at"`
}

type MetricType string
const (
    MetricConversion MetricType = "conversion" // binary: did the user convert?
    MetricNumeric    MetricType = "numeric"    // continuous: revenue, latency
    MetricCount      MetricType = "count"      // events per user
)

type Metric struct {
    Key            string     `json:"key"`
    Name           string     `json:"name"`
    Type           MetricType `json:"type"`
    EventKey       string     `json:"event_key"`
    Direction      string     `json:"direction"`   // "increase" | "decrease"
    IsGuardrail    bool       `json:"is_guardrail"`
}
```

### 11.2 Running statistics (Welford)

```go
// internal/stats/welford.go
// Numerically stable online mean/variance. Required because computing
// variance as E[X²] - E[X]² catastrophically loses precision when the
// mean is large relative to the variance (e.g. revenue metrics).

type RunningStats struct {
    N    int64
    Mean float64
    M2   float64  // sum of squared deviations from the running mean
}

func (r *RunningStats) Add(x float64) {
    r.N++
    delta := x - r.Mean
    r.Mean += delta / float64(r.N)
    delta2 := x - r.Mean
    r.M2 += delta * delta2
}

func (r *RunningStats) Variance() float64 {
    if r.N < 2 { return 0 }
    return r.M2 / float64(r.N-1)   // sample variance (Bessel-corrected)
}
func (r *RunningStats) StdDev() float64  { return math.Sqrt(r.Variance()) }
func (r *RunningStats) StdError() float64 { return r.StdDev() / math.Sqrt(float64(r.N)) }

// Merge: combine two partitions' stats (parallel aggregation)
func (r *RunningStats) Merge(o RunningStats) {
    if o.N == 0 { return }
    if r.N == 0 { *r = o; return }
    n := r.N + o.N
    delta := o.Mean - r.Mean
    mean := r.Mean + delta*float64(o.N)/float64(n)
    m2 := r.M2 + o.M2 + delta*delta*float64(r.N)*float64(o.N)/float64(n)
    r.N, r.Mean, r.M2 = n, mean, m2
}
```

### 11.3 Two-proportion z-test (conversion metrics)

```go
// internal/stats/ztest.go

type ProportionResult struct {
    ControlN, ControlConv     int64
    TreatmentN, TreatmentConv int64
    ControlRate, TreatmentRate float64
    AbsoluteEffect  float64   // p_t - p_c (percentage points)
    RelativeEffect  float64   // (p_t - p_c) / p_c  ("lift")
    ZScore          float64
    PValue          float64   // two-sided
    CILower, CIUpper float64  // CI on the ABSOLUTE effect
    Significant     bool
}

func TwoProportionZTest(cn, cc, tn, tc int64, alpha float64) ProportionResult {
    pc := float64(cc) / float64(cn)
    pt := float64(tc) / float64(tn)

    // Pooled proportion — used for the TEST STATISTIC (null hypothesis: p_c == p_t)
    pPool := float64(cc+tc) / float64(cn+tn)
    sePooled := math.Sqrt(pPool * (1 - pPool) * (1/float64(cn) + 1/float64(tn)))

    z := 0.0
    if sePooled > 0 { z = (pt - pc) / sePooled }
    p := 2 * (1 - normalCDF(math.Abs(z)))

    // UNPOOLED standard error — used for the CONFIDENCE INTERVAL.
    // Using the pooled SE for the CI is a classic and subtle error: the CI is
    // not computed under the null, so it must use each group's own variance.
    seUnpooled := math.Sqrt(pc*(1-pc)/float64(cn) + pt*(1-pt)/float64(tn))
    zCrit := normalQuantile(1 - alpha/2)
    diff := pt - pc

    rel := 0.0
    if pc > 0 { rel = diff / pc }

    return ProportionResult{
        ControlN: cn, ControlConv: cc, TreatmentN: tn, TreatmentConv: tc,
        ControlRate: pc, TreatmentRate: pt,
        AbsoluteEffect: diff, RelativeEffect: rel,
        ZScore: z, PValue: p,
        CILower: diff - zCrit*seUnpooled,
        CIUpper: diff + zCrit*seUnpooled,
        Significant: p < alpha,
    }
}

// normalCDF via the complementary error function — numerically stable in the tails,
// unlike a naive Taylor series which loses all precision for |z| > 6.
func normalCDF(z float64) float64 { return 0.5 * math.Erfc(-z/math.Sqrt2) }

// normalQuantile: inverse normal CDF (Acklam's rational approximation, |err| < 1.15e-9)
func normalQuantile(p float64) float64 { /* ... */ }
```

### 11.4 Sequential testing (the peeking problem)

Fixed-horizon tests assume you look **once**, at a pre-planned sample size. Product teams look constantly. Every peek at α=0.05 inflates the real Type I error — peek 10 times and your actual false-positive rate is closer to 20–30%.

```go
// internal/stats/msprt.go
// Mixture Sequential Probability Ratio Test — gives "always-valid" p-values
// that remain correct no matter how often you peek.
// (Johari, Pekelis, Walsh — "Always Valid Inference", Stanford/Optimizely)

type SequentialResult struct {
    AlwaysValidPValue float64
    ConfidenceSequenceLower float64
    ConfidenceSequenceUpper float64
    Decision          SeqDecision  // Continue | RejectNull | AcceptNull
    SamplesCollected  int64
    SamplesRequired   int64        // for futility / max-horizon
}

// mSPRT for two proportions with a normal mixing distribution of variance tau².
// tau is set from the experiment's MDE: tau = MDE / 2 is a reasonable default.
func MSPRT(cn, cc, tn, tc int64, tau, alpha float64) SequentialResult {
    pc := float64(cc) / float64(cn)
    pt := float64(tc) / float64(tn)
    delta := pt - pc

    // Variance of the difference under the observed data
    v := pc*(1-pc)/float64(cn) + pt*(1-pt)/float64(tn)
    if v <= 0 { return SequentialResult{Decision: SeqContinue, AlwaysValidPValue: 1.0} }

    n := float64(cn+tn) / 2 // effective per-arm n

    // Likelihood ratio under a N(0, tau²) mixture prior on the true effect
    lr := math.Sqrt(v/(v+tau*tau)) *
          math.Exp((tau*tau*delta*delta)/(2*v*(v+tau*tau)))

    // Always-valid p-value = min over time of 1/LR, clamped to [0,1]
    avP := math.Min(1.0, 1.0/lr)

    // Confidence sequence (invert the test)
    halfWidth := math.Sqrt(v * (v + tau*tau) / (tau * tau) *
                 math.Log((v+tau*tau)/(v*alpha*alpha)))

    d := SeqContinue
    if avP < alpha { d = SeqRejectNull }

    return SequentialResult{
        AlwaysValidPValue: avP,
        ConfidenceSequenceLower: delta - halfWidth,
        ConfidenceSequenceUpper: delta + halfWidth,
        Decision: d,
        SamplesCollected: cn + tn,
    }
}
```

The admin console shows **both**: the fixed-horizon p-value (with a "you have peeked N times" warning) and the always-valid p-value (safe to act on at any moment).

### 11.5 Sample Ratio Mismatch (SRM) detection

If you configured a 50/50 split and observe 50.3/49.7 with a million users, something is broken — bot traffic, a redirect that drops one arm, an SDK bug, or a mid-experiment targeting change. **SRM invalidates the entire experiment**; no amount of correct statistics fixes it.

```go
// internal/stats/srm.go

type SRMResult struct {
    Observed  []int64
    Expected  []float64
    ChiSquare float64
    PValue    float64
    Mismatch  bool     // p < 0.001 (deliberately strict — SRM is a data-quality alarm)
}

func CheckSRM(observed []int64, expectedWeights []float64) SRMResult {
    var total int64
    for _, o := range observed { total += o }
    var weightSum float64
    for _, w := range expectedWeights { weightSum += w }

    expected := make([]float64, len(expectedWeights))
    chi := 0.0
    for i, w := range expectedWeights {
        expected[i] = float64(total) * w / weightSum
        if expected[i] > 0 {
            d := float64(observed[i]) - expected[i]
            chi += d * d / expected[i]
        }
    }

    df := len(observed) - 1
    p := 1 - chiSquareCDF(chi, df)

    return SRMResult{
        Observed: observed, Expected: expected,
        ChiSquare: chi, PValue: p,
        Mismatch: p < 0.001,   // stricter than 0.05: false SRM alarms are very costly
    }
}
```

### 11.6 Sample size calculator

```go
// Required per-arm sample size for a two-proportion test.
// n = (z_{α/2} + z_β)² × [p_c(1-p_c) + p_t(1-p_t)] / (p_t - p_c)²
func RequiredSampleSize(baselineRate, mde, alpha, power float64) int64 {
    zAlpha := normalQuantile(1 - alpha/2)
    zBeta  := normalQuantile(power)
    pc := baselineRate
    pt := baselineRate + mde
    num := math.Pow(zAlpha+zBeta, 2) * (pc*(1-pc) + pt*(1-pt))
    den := math.Pow(pt-pc, 2)
    return int64(math.Ceil(num / den))
}
```

---

## §12 REST API

Base: `http://localhost:8080/api/v1`

### Projects & Environments
| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/projects` | List projects |
| `POST` | `/projects` | Create project |
| `GET` | `/projects/:proj/environments` | List environments |
| `POST` | `/projects/:proj/environments` | Create environment (returns SDK keys) |
| `POST` | `/projects/:proj/environments/:env/rotate-key` | Rotate SDK key |
| `POST` | `/projects/:proj/environments/:env/clone-from/:src` | Copy all flag configs from another env |

### Flags
| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/projects/:proj/flags` | List flags (filter by tag/status/type) |
| `POST` | `/projects/:proj/flags` | Create flag |
| `GET` | `/projects/:proj/flags/:key` | Flag detail (all environments) |
| `PATCH` | `/projects/:proj/flags/:key` | Update name/description/tags/variations |
| `DELETE` | `/projects/:proj/flags/:key` | Archive flag |
| `PUT` | `/projects/:proj/flags/:key/environments/:env` | Replace env config (targeting, rules, rollout) |
| `POST` | `/projects/:proj/flags/:key/environments/:env/toggle` | Kill switch: `{on: bool}` |
| `GET` | `/projects/:proj/flags/:key/environments/:env/diff/:otherEnv` | Config diff between environments |

### Segments
| Method | Path | Description |
|--------|------|-------------|
| `GET` `POST` | `/projects/:proj/segments` | List / create |
| `GET` `PUT` `DELETE` | `/projects/:proj/segments/:key` | Read / update / delete |

### Evaluation (server-side / debugging)
| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/eval/:env/:flagKey` | Evaluate one flag: `{context}` → `{value, variation, reason}` |
| `POST` | `/eval/:env` | Evaluate all flags for a context |
| `POST` | `/eval/:env/:flagKey/explain` | Full trace: every step, every clause result |
| `POST` | `/eval/:env/:flagKey/simulate` | Run against N synthetic contexts → variation distribution |

### SDK endpoints
| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/sdk/stream/:env` | **SSE stream** (supports `Last-Event-ID`) |
| `GET` | `/sdk/snapshot/:env` | Full snapshot (polling mode / initial fetch) |
| `POST` | `/sdk/events` | Batched event ingestion |
| `GET` | `/sdk/bootstrap/:env` | Pre-evaluated flags for a context (client-side SDK / SSR) |

### Experiments
| Method | Path | Description |
|--------|------|-------------|
| `GET` `POST` | `/projects/:proj/experiments` | List / create |
| `GET` | `/projects/:proj/experiments/:key` | Detail |
| `POST` | `/projects/:proj/experiments/:key/start` | Start |
| `POST` | `/projects/:proj/experiments/:key/stop` | Stop |
| `GET` | `/projects/:proj/experiments/:key/results` | Full stats: z-test, mSPRT, CIs, SRM |
| `POST` | `/experiments/sample-size` | Sample size calculator |

### Analytics & Ops
| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/projects/:proj/flags/:key/insights` | Eval counts, variation distribution, over time |
| `GET` | `/projects/:proj/flags/stale` | Flags not evaluated in N days, or 100% rolled out |
| `GET` | `/projects/:proj/audit` | Audit log (filter by flag/user/action) |
| `GET` | `/ops/connections` | Live SSE subscribers: env, SDK version, connected-at, lag |
| `GET` | `/ops/metrics` | Propagation latency p50/p99, publish rate, slow-client disconnects |
| `GET` | `/scenarios` · `POST` `/scenarios/:name/run` | Demo scenarios |

**Admin SSE:** `GET /api/v1/stream` — pushes flag-changed / experiment-updated / connection events to the console.

---

## §13 Frontend — Eight Views

### View 1: Flag List
TanStack Table with: key, name, type, per-environment on/off pills, variation count, tags, last evaluated, staleness badge.
- Column filters, global search, saved views
- Bulk actions: tag, archive
- Inline kill-switch toggle per environment (with confirm dialog for production)
- Stale-flag callout: "12 flags at 100% rollout for >30 days — candidates for removal"

### View 2: Flag Detail — Targeting Editor ⭐ (the centerpiece)

The most complex UI in the project. Layout mirrors the evaluation order so the UI *teaches* the algorithm:

```
┌────────────────────────────────────────────────────────────┐
│  new-checkout-flow                    [production ▾]  [ON ●]│
├────────────────────────────────────────────────────────────┤
│  ① Prerequisites                                    [+ Add] │
│     ▸ billing-v2 must be  [treatment ▾]              [[X]]   │
├────────────────────────────────────────────────────────────┤
│  ② Individual targets                               [+ Add] │
│     ▸ Serve [treatment ▾] to: [user-123 ×][user-456 ×]     │
├────────────────────────────────────────────────────────────┤
│  ③ Targeting rules   (first match wins — drag to reorder)   │
│     ⣿ Rule 1: internal beta                    [[X]] [⋮]     │
│        IF  [email ▾] [ends with ▾] [@acme.com ×]           │
│        AND [plan  ▾] [is one of ▾] [enterprise ×][pro ×]   │
│        THEN serve [treatment ▾]                             │
│                                                             │
│     ⣿ Rule 2: gradual rollout                  [[X]] [⋮]     │
│        IF  [country ▾] [is one of ▾] [US ×][CA ×]          │
│        THEN percentage rollout:                             │
│            control    ▓▓▓▓▓▓▓▓░░░░░░░░░░░░  80%  [80000]    │
│            treatment  ▓▓▓▓░░░░░░░░░░░░░░░░  20%  [20000]    │
│                                        Σ = 100% [ok]           │
│                                    bucket by [key ▾]        │
│                                             [+ Add rule]    │
├────────────────────────────────────────────────────────────┤
│  ④ Default rule (everyone else)                             │
│        Serve [control ▾]                                    │
├────────────────────────────────────────────────────────────┤
│  When OFF, serve: [control ▾]                               │
├────────────────────────────────────────────────────────────┤
│  [WARNING] 3 unsaved changes    [Review diff]  [Discard] [Save →]  │
└────────────────────────────────────────────────────────────┘
```

Implementation notes:
- **dnd-kit** `SortableContext` for rule reordering — order is semantic (first match wins), so this is functional, not cosmetic
- **react-hook-form** `useFieldArray` nested two deep (rules → clauses)
- **zod** schema validates weights sum to exactly 100000; the Save button is disabled until valid
- Percentage sliders are linked: dragging one redistributes the remainder proportionally
- Operator dropdown filters by inferred attribute type (semver operators only for version-ish attributes)
- **Save shows a diff dialog first** — production flag changes are dangerous; never silently apply
- Optimistic UI + rollback on API error

### View 3: Evaluation Debugger

Split pane. Left: a context editor (Monaco, JSON). Right: a live step-by-step trace.

```
Context: {"kind":"user","key":"user-4821","attributes":{"email":"a@acme.com","plan":"pro","country":"US"}}

TRACE for new-checkout-flow @ production
  ① Flag is ON                                                    [ok]
  ② Prerequisite billing-v2 → treatment (required: treatment)     [ok] pass
  ③ Individual targets: user-4821 not in any target list          → continue
  ④ Rule 1 "internal beta"
       email endsWith "@acme.com"   → "a@acme.com"   MATCH   [ok]
       plan in [enterprise, pro]    → "pro"          MATCH   [ok]
       ⇒ RULE MATCHED  →  serve variation 1 (treatment)

RESULT   value = true    variation = 1 (treatment)
         reason = { kind: RULE_MATCH, ruleIndex: 0, ruleId: "r-9f2a" }
         bucket = n/a (fixed variation)
```

- Preset contexts (saved test users)
- "Why not X?" — pick any variation, get an explanation of what would need to change
- **Bulk simulate**: generate 10,000 synthetic contexts, show the resulting variation histogram against the configured weights (catches misconfigured rollouts before they ship)

### View 4: Real-Time Propagation Monitor

The view that makes "real-time" tangible.

- Live-updating list of connected SDKs: environment, SDK name + version, connected duration, version lag, events received
- **Propagation timeline**: toggle a flag, watch a horizontal timeline fill in as each connected SDK acknowledges the new version. Target: all green in <200 ms.
- Histogram of propagation latency (p50 / p95 / p99)
- Publish rate chart; slow-client disconnect counter
- **Chaos controls** (demo): kill the SSE hub, force a reconnect storm, inject 500 ms of network latency, drop a client's connection — watch the SDKs backoff-and-recover

### View 5: Experiments

**List:** status, flag, primary metric, sample progress bar (collected / required), current lift, significance chip.

**Detail:**
- Results table per variation: users, conversions, rate, lift vs control, CI, p-value
- **Conversion-rate-over-time chart** (Recharts) with confidence bands per variation
- **Two p-value displays side by side:**
  - Fixed-horizon p-value + `[WARNING] You have viewed these results 14 times. Fixed-horizon p-values are only valid at the pre-planned sample size.`
  - Always-valid (mSPRT) p-value + `[ok] Safe to act on at any time.`
- **SRM banner** (red, dismissal-blocking): `[WARNING] SAMPLE RATIO MISMATCH: expected 50.0/50.0, observed 51.4/48.6 (χ²=42.7, p=0.0000006). Results are not trustworthy. Investigate assignment before drawing conclusions.`
- Guardrail metrics section — flags regressions even when the primary metric wins
- Sample size calculator inline: baseline rate + MDE + α + power → required n and estimated days at current traffic

### View 6: Segments
Reusable targeting definitions with the same clause builder as View 2. Live count: "≈ 12,430 of 100,000 sampled contexts match".

### View 7: Audit Log & Diffs
Chronological change feed: who, when, what. Each entry expands to a **JSON diff** (added green / removed red / changed amber) of the flag config before → after. One-click **revert to this version**.

### View 8: Scenario Runner + Metrics

| Scenario | Demonstrates |
|----------|-------------|
| `kill_switch` | Toggle off; measure propagation to all SDKs; assert <200 ms |
| `gradual_rollout` | 1% → 5% → 25% → 50% → 100%; show that no user ever loses the feature (monotonicity) |
| `targeting_rules` | Same context evaluated against 6 rule configurations, side by side |
| `prerequisite_chain` | 3-level prerequisite chain; break the root; watch all dependents fall back to off |
| `sticky_bucketing` | Same user evaluated 1000× across restarts → always identical variation |
| `bucket_distribution` | 100k synthetic users → histogram vs configured weights; χ² goodness-of-fit |
| `sdk_parity` | Same fixtures through Go SDK and TS SDK → assert byte-identical results |
| `reconnect_storm` | Kill the hub; 500 SDKs reconnect with jittered backoff; no thundering herd |
| `delta_vs_snapshot` | Client reconnects with old `Last-Event-ID`; show delta path vs full-snapshot fallback |
| `experiment_run` | Simulate 50k users with a true 2% lift; watch p-value converge; compare fixed vs sequential |
| `srm_injection` | Deliberately break assignment 52/48; SRM detector fires |
| `peeking_problem` | Run 100 A/A tests (no real effect); show fixed-horizon false-positive rate ≈ 25% with daily peeking vs ≈ 5% with mSPRT |

**Metrics dashboard:** evaluations/sec, propagation p50/p99, connected SDKs, snapshot size, delta compression ratio, event ingestion rate, experiment count.

---

## §14 File Structure

```
pennant/
├── cmd/
│   ├── server/main.go
│   └── loadgen/main.go              # synthetic SDK fleet for demos
├── internal/
│   ├── model/                       # Flag, Segment, Context, Rule, Clause, Rollout
│   ├── eval/
│   │   ├── evaluate.go              # the normative algorithm
│   │   ├── clause.go                # 14 operators
│   │   ├── segment.go
│   │   ├── bucket.go                # ComputeBucket — the critical function
│   │   ├── operators.go             # semver, datetime, regex comparisons
│   │   └── explain.go               # step-by-step trace for the debugger
│   ├── store/                       # ConfigStore: flags, segments, versions, audit
│   ├── snapshot/                    # builder, canonical JSON, checksum, delta computation
│   ├── stream/                      # Hub, Subscriber, EventRing, SSE writer
│   ├── sdkauth/                     # SDK key validation, env scoping
│   ├── analytics/                   # ingestion, dedupe, aggregation, insights
│   ├── stats/
│   │   ├── welford.go
│   │   ├── ztest.go
│   │   ├── msprt.go
│   │   ├── srm.go
│   │   ├── samplesize.go
│   │   └── distributions.go         # normalCDF, normalQuantile, chiSquareCDF
│   ├── experiment/                  # lifecycle, assignment, results assembly
│   ├── audit/
│   ├── simulation/                  # 12 scenarios
│   └── events/                      # internal bus
├── gateway/                         # server.go, rest.go, sse.go, sdk.go
├── sdk/
│   ├── go/                          # client, streamer, evaluator, events, store
│   └── ts/                          # same, in TypeScript
├── conformance/                     # ~200 shared JSON fixtures
│   ├── off/ targets/ clauses/ rules/ rollout/ segments/ prerequisites/
│   ├── bucketing/ canonical_json/
│   └── README.md
├── EVALUATION.md                    # normative spec — the SDK contract
├── frontend/
│   ├── vite.config.ts
│   ├── tailwind.config.ts
│   ├── components.json              # shadcn/ui config
│   └── src/
│       ├── api/{client,queries,types,schemas}.ts
│       ├── sse/client.ts
│       ├── components/
│       │   ├── ui/                  # shadcn primitives
│       │   ├── flags/               # FlagList, FlagDetail
│       │   ├── targeting/           # RuleBuilder, ClauseEditor, RolloutSlider, PrereqEditor
│       │   ├── debugger/            # EvaluationDebugger, TraceView, BulkSimulate
│       │   ├── monitor/             # PropagationMonitor, ConnectionList, LatencyChart
│       │   ├── experiments/         # ExperimentList, ResultsPanel, SRMBanner, SampleSizeCalc
│       │   ├── segments/
│       │   ├── audit/               # AuditLog, JsonDiff
│       │   └── scenarios/
│       └── lib/{format,diff,validation}.ts
├── test/{unit,integration}/
├── config.yaml
├── go.mod
└── Makefile
```

---

## §15 Configuration

```yaml
server:
  port: 8080
  cors_origins: ["http://localhost:5173"]

store:
  type: "sqlite"                # memory | sqlite
  path: "./data/flags.db"

stream:
  heartbeat_interval: "25s"     # under typical 30s proxy idle timeout
  subscriber_buffer: 32         # per-client channel capacity
  replay_ring_size: 256         # deltas retained per environment
  max_connections_per_env: 50000
  slow_client_policy: "disconnect"   # disconnect | drop_events

snapshot:
  delta_threshold: 0.30         # if delta > 30% of snapshot size, send full snapshot
  compression: "gzip"
  checksum_algorithm: "sha256"

evaluation:
  max_prerequisite_depth: 20    # cycle guard
  bucket_scale: 1152921504606846975  # 2^60 - 1

analytics:
  ingest_buffer: 4096
  ingest_workers: 4
  batch_size: 512
  flush_interval: "1s"
  dedupe_window: "60s"

experiments:
  default_alpha: 0.05
  default_power: 0.80
  srm_threshold: 0.001          # strict: false SRM alarms are costly
  msprt_tau_from_mde: 0.5       # tau = mde * 0.5

sdk:
  default_poll_interval: "30s"
  events_flush_interval: "5s"
  events_capacity: 10000

frontend:
  dev_port: 5173
```

---

## §16 Correctness Properties

1. **Cross-SDK determinism.** For every conformance fixture, the Go SDK and the TypeScript SDK return the identical variation index, value, and reason. CI fails on any divergence.

2. **Bucketing stability.** For a fixed `(flagKey, salt, contextKey, bucketBy)`, `ComputeBucket` returns the same float forever, in every language, on every platform. Locked by hardcoded fixtures.

3. **Rollout monotonicity.** Increasing a variation's weight only adds contexts to that variation; no context that had it loses it. (Users never see a feature disappear during a ramp-up.)

4. **First-match-wins ordering.** Rules are evaluated in stored order; the first matching rule determines the result. Reordering rules in the UI changes evaluation outcomes — and the UI makes that visible.

5. **Target precedence.** Individual targets are checked before rules and always win.

6. **Prerequisite correctness.** A flag serves its off-variation if any prerequisite is off or serving the wrong variation. Prerequisite cycles are detected and return `PREREQUISITE_CYCLE`, never hang.

7. **Snapshot atomicity.** An SDK's view of config is always a complete, checksum-valid snapshot at some version. Partial application is impossible: checksum mismatch → discard and refetch.

8. **Propagation bound.** A flag change reaches every healthy connected SDK within 200 ms (p99, local network).

9. **Fail-safe degradation.** No failure mode of the flag service causes an exception in the calling application. Worst case: caller-supplied defaults with reason `CLIENT_NOT_READY`.

10. **No blocking on slow clients.** A single slow SSE subscriber never delays delivery to others; it is disconnected and reconnects with a fresh snapshot.

11. **Event-loss tolerance.** Analytics events are best-effort. Dropping events degrades experiment precision; it never affects flag evaluation correctness.

12. **Statistical validity.** Confidence intervals use the unpooled SE; the test statistic uses the pooled SE. Always-valid p-values are exposed whenever results are viewed more than once.

---

## §17 Performance Targets

| Metric | Target |
|--------|--------|
| Flag evaluation (SDK, in-memory, no rollout) | < 1 µs |
| Flag evaluation (with SHA-1 bucketing) | < 5 µs |
| Flag evaluation (server-side REST) | < 1 ms p99 |
| Propagation: admin toggle → SDK applied | < 200 ms p99 |
| Concurrent SSE connections (single instance) | 10,000+ |
| Memory per SSE connection | < 8 KiB |
| Snapshot build (1,000 flags) | < 10 ms |
| Delta computation | < 1 ms |
| Event ingestion | > 50,000 events/s |
| Experiment results computation (1M events) | < 500 ms |
| Admin console flag list (1,000 flags) | < 100 ms render |
| Bulk simulate (10,000 contexts) | < 200 ms |
