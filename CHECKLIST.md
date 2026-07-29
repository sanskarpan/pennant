# CHECKLIST.md — Feature Flag Service with Real-Time Updates

> Priority: [P0] blocking · [P1] important · [P2] enhancement · [P3] stretch
> **Phase 2 (evaluation engine) and Phase 3 (conformance suite) are the spine of this project. Nothing else matters if evaluation is wrong or the two SDKs disagree.**

---

## Phase 0 — Bootstrap (14 tasks)

**Go backend**
- [ ] [P0] `go mod init pennant`; add `go-chi/chi/v5`, `Masterminds/semver/v3`, `modernc.org/sqlite`, `stretchr/testify`, `golang.org/x/sync/errgroup`
- [ ] [P0] Directory structure: `cmd/{server,loadgen}/`, `internal/{model,eval,store,snapshot,stream,sdkauth,analytics,stats,experiment,audit,simulation,events}/`, `gateway/`, `sdk/{go,ts}/`, `conformance/`, `test/`
- [ ] [P0] `config.yaml` with all defaults from SPEC §15
- [ ] [P0] `internal/events/bus.go`: non-blocking Publish / Subscribe / Unsubscribe
- [ ] [P0] `Makefile`: `run`, `test`, `test-race`, `conformance`, `lint`, `frontend-dev`, `loadgen`
- [ ] [P0] `cmd/server/main.go`: load config, open store, build snapshot, start hub + gateway + analytics

**React frontend**
- [ ] [P0] `cd frontend && bun create vite . --template react-ts`
- [ ] [P0] `bun add @tanstack/react-query @tanstack/react-table react-hook-form zod @hookform/resolvers @dnd-kit/core @dnd-kit/sortable recharts @monaco-editor/react react-router-dom clsx tailwind-merge lucide-react date-fns`
- [ ] [P0] `bun add -d tailwindcss postcss autoprefixer`; `bunx tailwindcss init -p`
- [ ] [P0] `bunx shadcn@latest init`; add components: `button card dialog select combobox input badge table tabs switch slider popover tooltip alert sheet dropdown-menu form separator skeleton`
- [ ] [P0] `vite.config.ts`: proxy `/api` → `http://localhost:8080` (SSE needs no special config — plain HTTP)
- [ ] [P0] `src/api/types.ts`: TypeScript mirror of all Go model types
- [ ] [P0] `src/api/schemas.ts`: zod schemas for Flag, Rule, Clause, Rollout — reused by both forms and API validation
- [ ] [P0] `src/App.tsx`: router (8 routes), QueryClientProvider, SSE connection on mount

---

## Phase 1 — Data Model (18 tasks)

- [ ] [P0] `internal/model/flag.go`: `VariationType` enum, `Variation`, `Flag`, `FlagConfig`
- [ ] [P0] `Prerequisite`, `Target`, `Rule`, `VariationOrRollout`, `Rollout`, `WeightedVariation`
- [ ] [P0] **Weights are 0–100000, not 0–100** — enables exact 33.333% thirds and 0.001% canaries
- [ ] [P0] `internal/model/clause.go`: `Clause` + all 14 `Operator` constants
- [ ] [P0] `internal/model/segment.go`: `Segment`, `SegmentRule` with `Included`/`Excluded`/`Rules`
- [ ] [P0] `internal/model/context.go`: `Context{Kind, Key, Anonymous, Attributes, Private}`
- [ ] [P0] `Context.GetAttribute(name)`: built-ins (`key`, `kind`, `anonymous`) shadow custom attributes
- [ ] [P0] `internal/model/reason.go`: `ReasonKind`, `ErrorKind`, `Reason` structs
- [ ] [P0] `internal/model/environment.go`: `Project`, `Environment`, SDK key types (server/client/mobile)
- [ ] [P0] JSON marshal/unmarshal tags matching the wire format exactly
- [ ] [P0] `Flag.Validate()`: ≥2 variations, variation indices in range, weights sum to 100000, no duplicate rule IDs
- [ ] [P0] `Flag.ValidateVariationTypes()`: all variation values match the declared `Type`
- [ ] [P0] `Rule.Validate()`: ≥1 clause, exactly one of `Variation`/`Rollout` set
- [ ] [P0] `Segment.Validate()`: no key in both Included and Excluded
- [ ] [P0] Unit test `TestFlag_ValidateWeightSum`: 33334+33333+33333 = 100000 ✓; 33333×3 ✗
- [ ] [P0] Unit test `TestFlag_ValidateVariationIndices`: rule referencing variation 5 of a 2-variation flag → error
- [ ] [P0] Unit test `TestContext_BuiltInAttributesShadow`: attribute named `key` does not override `Context.Key`
- [ ] [P0] `go test ./internal/model/... -race` zero races

---

## Phase 2 — Evaluation Engine (34 tasks)

**The single most important phase. Implement SPEC §5 exactly.**

**Bucketing**
- [ ] [P0] `internal/eval/bucket.go`: `BucketScale = 0xFFFFFFFFFFFFFFF` (2^60−1)
- [ ] [P0] `ComputeBucket(ctx, bucketBy, key, salt, seed) float64` returning `[0.0, 1.0)`
- [ ] [P0] Hash input: `seed != nil` → `"{seed}.{id}"`; else `"{key}.{salt}.{id}"`
- [ ] [P0] SHA-1, take first **15 hex chars** (60 bits), parse as int64, divide by `BucketScale`
- [ ] [P0] `stringifyBucketValue`: strings ✓, integers ✓, **integral floats ✓, non-integral floats ✗, bools ✗** (cross-language string-repr trap)
- [ ] [P0] Missing `bucketBy` attribute → bucket 0.0 (deterministic, not an error)
- [ ] [P0] Unit test `TestBucket_Deterministic`: same inputs 10,000× → identical output
- [ ] [P0] Unit test `TestBucket_Uniform`: 100,000 keys → χ² goodness-of-fit across 100 bins, p > 0.01
- [ ] [P0] Unit test `TestBucket_IndependentPerFlag`: same user across 50 flags → correlation ≈ 0
- [ ] [P0] Unit test `TestBucket_Monotonic`: users in a 10% rollout are a strict subset of users in the 20% rollout
- [ ] [P0] Unit test `TestBucket_KnownValues`: hardcoded input→output pairs (locks the hash function permanently)

**Operators**
- [ ] [P0] `internal/eval/operators.go`: `matchOperator(op, attrValue, clauseValue) bool`
- [ ] [P0] `in` — exact equality with type coercion rules (string≠number, no loose equality)
- [ ] [P0] `startsWith`, `endsWith`, `contains` — string-only; non-string attribute → no match
- [ ] [P0] `matches` — regex; **compile once and cache**; invalid regex → no match (never panic)
- [ ] [P0] `lessThan`, `lessThanOrEqual`, `greaterThan`, `greaterThanOrEqual` — numeric only
- [ ] [P0] `before`, `after` — RFC3339 strings **or** Unix-millis numbers; parse failure → no match
- [ ] [P0] `semVerEqual`, `semVerLessThan`, `semVerGreaterThan` via `Masterminds/semver`; unparseable → no match
- [ ] [P0] `segmentMatch` — handled in `clauseMatches`, not `matchOperator`
- [ ] [P0] Unit test: every operator × (match, no-match, wrong-type, missing-attribute) = 56 cases

**Clauses & rules**
- [ ] [P0] `clauseMatches`: OR across `Values`; OR across array elements if the attribute is an array
- [ ] [P0] **Missing attribute → `false` BEFORE negation is applied** (negation does not resurrect missing attrs)
- [ ] [P0] `Negate` inverts the final clause result
- [ ] [P0] `ruleMatches`: AND across all clauses
- [ ] [P0] Unit test `TestClause_NegateMissingAttribute`: negated clause on a missing attribute → still no match

**Segments**
- [ ] [P0] `internal/eval/segment.go`: `segmentMatches` — **Excluded wins over Included**
- [ ] [P0] Segment rules with optional `Weight`: bucket against segment key + salt
- [ ] [P0] Unit test `TestSegment_ExcludedBeatsIncluded`: key in both lists → not in segment

**Main algorithm**
- [ ] [P0] `internal/eval/evaluate.go`: `Evaluate(flag, ctx, store) (idx, value, reason)` — SPEC §5 step order exactly
- [ ] [P0] Step 1 OFF: `OffVariation == nil` → default value with reason `OFF`
- [ ] [P0] Step 2 prerequisites: recursive evaluation; **depth guard = 20** + visited-set cycle detection → `PREREQUISITE_CYCLE`
- [ ] [P0] Step 3 individual targets: checked **before** rules, always win
- [ ] [P0] Step 4 rules: **first match wins**, in stored order
- [ ] [P0] Step 5 fallthrough
- [ ] [P0] `resolveVariationOrRollout`: cumulative weight walk; **floating-point safety net** returns the last variation if `bucket` rounds to 1.0
- [ ] [P0] `internal/eval/explain.go`: `Explain(flag, ctx, store) Trace` — every step, every clause result, for the debugger
- [ ] [P0] `go test ./internal/eval/... -race -count=3` zero races

---

## Phase 3 — Conformance Suite (16 tasks)

**The highest-value artifact in the project.**

- [ ] [P0] `EVALUATION.md`: normative, language-neutral spec (copy of SPEC §5 with RFC-2119 MUST/SHOULD)
- [ ] [P0] `conformance/schema.json`: JSON Schema for fixture files (validated in CI)
- [ ] [P0] Fixture format: `{name, flag, segments?, cases: [{context, expect: {variation, value, reason}}]}`
- [ ] [P0] `conformance/off/` — 8 fixtures: off with/without offVariation, archived
- [ ] [P0] `conformance/targets/` — 12 fixtures: individual targeting, precedence over rules
- [ ] [P0] `conformance/clauses/` — 60 fixtures: 14 operators × types × negation × missing attributes
- [ ] [P0] `conformance/rules/` — 25 fixtures: ordering, multi-clause AND, first-match-wins, rule→rollout
- [ ] [P0] `conformance/rollout/` — 30 fixtures: weight distribution, bucketBy, seed, integral floats, 0-weight variations
- [ ] [P0] `conformance/segments/` — 25 fixtures: included/excluded, segment rules, weighted, segment-in-clause
- [ ] [P0] `conformance/prerequisites/` — 15 fixtures: pass, fail, prereq-off, nested chains, cycles
- [ ] [P0] `conformance/bucketing/` — 20 fixtures with **hardcoded expected bucket floats** (permanently locks the hash)
- [ ] [P0] `conformance/canonical_json/` — 5 fixtures: checksum stability across languages
- [ ] [P0] `sdk/go/conformance_test.go`: walk `conformance/**/*.json`, run every case, assert exact match
- [ ] [P0] `sdk/ts/test/conformance.test.ts`: the **same** fixtures, same assertions
- [ ] [P0] CI job `conformance`: run both; fail the build on any divergence
- [ ] [P0] `conformance/README.md`: how to add a fixture; the rule that a fixture is never edited once merged (only added to)

---

## Phase 4 — Config Store & Snapshots (20 tasks)

- [ ] [P0] `internal/store/store.go`: `ConfigStore` interface (projects, environments, flags, segments, versions, audit)
- [ ] [P0] `internal/store/memory.go`: in-memory implementation with `sync.RWMutex`
- [ ] [P1] `internal/store/sqlite.go`: SQLite implementation (`modernc.org/sqlite`, pure Go, no cgo)
- [ ] [P0] Monotonic per-environment version counter; every write increments it
- [ ] [P0] `internal/snapshot/builder.go`: `BuildSnapshot(envKey) *Snapshot` — resolve flags + segments for one environment
- [ ] [P0] `internal/snapshot/canonical.go`: canonical JSON — **sorted keys, no insignificant whitespace, shortest round-trip numbers**
- [ ] [P0] `Checksum(snapshot) string`: SHA-256 of canonical JSON, prefixed `sha256:`
- [ ] [P0] `internal/snapshot/delta.go`: `ComputeDelta(from, to *Snapshot) *Delta`
- [ ] [P0] Delta contains upserted + deleted flags/segments and the **resulting** snapshot's checksum
- [ ] [P0] `delta_threshold`: if delta size > 30% of snapshot size, publish a full `put` instead of a `patch`
- [ ] [P0] `SnapshotPublisher`: on any config write → rebuild snapshot → compute delta → publish to hub
- [ ] [P0] Publishing is atomic: readers always see a complete snapshot at some version, never a half-applied state
- [ ] [P0] `internal/audit/audit.go`: `AuditEntry{Actor, Action, Resource, Before, After, At}` recorded on every mutation
- [ ] [P0] `internal/store/diff.go`: structural JSON diff for the audit UI and env-to-env comparison
- [ ] [P1] Revert: apply a historical audit entry's `Before` as a new change
- [ ] [P0] Unit test `TestCanonicalJSON_Stable`: same logical snapshot, different map iteration orders → identical bytes
- [ ] [P0] Unit test `TestChecksum_DetectsChange`: flip one boolean → checksum changes
- [ ] [P0] Unit test `TestDelta_Roundtrip`: `apply(from, ComputeDelta(from,to))` → equals `to`, checksums match
- [ ] [P0] Unit test `TestDelta_ThresholdFallsBackToFull`: large change → publisher emits `put` not `patch`
- [ ] [P0] `go test ./internal/{store,snapshot}/... -race` zero races

---

## Phase 5 — SSE Streaming Hub (22 tasks)

- [ ] [P0] `internal/stream/hub.go`: `Hub{byEnv map[string]map[string]*Subscriber, ring map[string]*EventRing}`
- [ ] [P0] `Subscriber{ID, EnvKey, SDKKey, Ch chan Message (cap 32), LastSent, ConnectedAt, UserAgent, SDKVersion}`
- [ ] [P0] `Hub.Subscribe(envKey, sdkKey, ua) *Subscriber`; `Hub.Unsubscribe(envKey, id)`
- [ ] [P0] `Hub.Publish(envKey, msg)`: **non-blocking** `select { case ch <- msg: default: }`
- [ ] [P0] **Slow-client policy**: full buffer → mark for disconnect, never block the publisher
- [ ] [P0] `internal/stream/ring.go`: `EventRing` — fixed-capacity circular buffer of recent deltas per environment
- [ ] [P0] `EventRing.Since(version) ([]Message, ok bool)`: `ok=false` if the version was evicted → caller sends a full snapshot
- [ ] [P0] `gateway/sse.go`: `HandleStream` — headers `text/event-stream`, `no-cache`, `keep-alive`, **`X-Accel-Buffering: no`**
- [ ] [P0] `http.Flusher` assertion; flush after the header write and after every event
- [ ] [P0] Parse `Last-Event-ID` header → replay deltas, or fall back to a full `put`
- [ ] [P0] SSE frame writer: `event: {type}\nid: {version}\ndata: {json}\n\n`
- [ ] [P0] Heartbeat every 25 s: write `: heartbeat\n\n` (SSE comment) and flush
- [ ] [P0] Exit cleanly on `r.Context().Done()`; always `Unsubscribe` via defer
- [ ] [P0] `internal/sdkauth/auth.go`: validate SDK key → environment scope; server vs client key distinction
- [ ] [P0] Client-side SDK keys must **not** be able to fetch full rule payloads (only pre-evaluated bootstrap)
- [ ] [P0] Connection metrics: active count per env, publish rate, slow-client disconnects, propagation latency histogram
- [ ] [P1] `GET /ops/connections`: live subscriber list for the monitor view
- [ ] [P0] Unit test `TestHub_NonBlockingPublish`: one subscriber never reads; publish 1000 msgs; other subscribers still receive all
- [ ] [P0] Unit test `TestRing_ReplayWindow`: request an evicted version → `ok=false`
- [ ] [P0] Unit test `TestRing_ReplayDeltas`: request version N → receive exactly deltas N+1..max
- [ ] [P0] Integration test `TestSSE_EndToEnd`: connect, receive `put`, toggle a flag, receive `patch` within 200 ms
- [ ] [P0] `go test ./internal/stream/... -race -count=5` zero races

---

## Phase 6 — Go SDK (22 tasks)

- [ ] [P0] `sdk/go/config.go`: `Config{SDKKey, BaseURL, StreamURL, EventsURL, Mode, PollInterval, InitTimeout, ...}`
- [ ] [P0] `sdk/go/client.go`: `Client` with `store *atomic.Pointer[Snapshot]` — **lock-free reads on the hot path**
- [ ] [P0] `NewClient(cfg)`: start streamer + event processor; return immediately (non-blocking construction)
- [ ] [P0] `WaitForInit(timeout) error`: blocks for the first snapshot; **on timeout the client is usable but degraded**, never fatal
- [ ] [P0] `sdk/go/stream.go`: SSE client — parse `event:`/`id:`/`data:` frames, handle `: comment` heartbeats
- [ ] [P0] Track `Last-Event-ID`; send it on reconnect
- [ ] [P0] Exponential backoff with **full jitter** (base 1 s, max 30 s) — prevents reconnect storms
- [ ] [P0] Apply `put` (replace snapshot) and `patch` (apply delta); **verify checksum after apply**
- [ ] [P0] Checksum mismatch → discard local state, drop `Last-Event-ID`, refetch full snapshot (self-healing)
- [ ] [P0] Polling mode: `GET /sdk/snapshot/:env` every `PollInterval` with ETag support
- [ ] [P0] Offline mode: load a snapshot from a file, no network at all (tests, air-gapped)
- [ ] [P0] `BoolVariation`, `StringVariation`, `IntVariation`, `Float64Variation`, `JSONVariation`
- [ ] [P0] `*VariationDetail` variants returning `Reason`
- [ ] [P0] Type mismatch → return the caller's default with `ErrWrongType` (never panic, never coerce silently)
- [ ] [P0] `AllFlags(ctx)`: bootstrap payload for client-side rendering
- [ ] [P0] `Track(eventKey, ctx, value, data)`: custom conversion events
- [ ] [P0] `sdk/go/events.go`: `EventProcessor` — **summary counters** for normal evals, individual records only for `inExperiment`
- [ ] [P0] Batched flush every 5 s or at capacity; drop-oldest on overflow (never block the caller)
- [ ] [P0] Private attributes: strip `Config.PrivateAttributes` and `Context.Private` from all outbound events
- [ ] [P0] `Close()`: flush events, close stream, stop goroutines
- [ ] [P0] Unit test `TestSDK_DegradedWhenNotReady`: no server → `BoolVariation` returns the default, reason `CLIENT_NOT_READY`
- [ ] [P0] `go test ./sdk/go/... -race -count=3` zero races

---

## Phase 7 — TypeScript SDK (18 tasks)

- [ ] [P0] `sdk/ts/src/types.ts`: mirror all Go model types (generated or hand-kept in sync)
- [ ] [P0] `sdk/ts/src/evaluate.ts`: **port SPEC §5 exactly** — same step order, same edge cases
- [ ] [P0] `sdk/ts/src/bucket.ts`: SHA-1 via `crypto.subtle` (browser) / `node:crypto` (server)
- [ ] [P0] **Parity trap**: `BigInt` for the 60-bit hash prefix — JS `Number` loses precision above 2^53
- [ ] [P0] `stringifyBucketValue` matching Go exactly, including the integral-float rule
- [ ] [P0] `sdk/ts/src/operators.ts`: all 14 operators; semver via a tiny hand-rolled comparator (avoid dependency drift)
- [ ] [P0] `sdk/ts/src/client.ts`: `FeatureFlagClient` with the same public surface as the Go SDK
- [ ] [P0] `EventSource` for streaming in the browser; `fetch` + `ReadableStream` for Node (custom auth headers)
- [ ] [P0] Exponential backoff with jitter (`EventSource` auto-reconnect is not configurable enough — wrap it)
- [ ] [P0] Checksum verification after delta apply; mismatch → full refetch
- [ ] [P0] `bootstrap` config option: hydrate from an SSR-injected snapshot with zero initial network wait
- [ ] [P0] Typed variation methods + `*Detail` variants
- [ ] [P0] `on('ready' | 'update' | 'error' | 'reconnecting')` event emitter
- [ ] [P0] Event buffering + batched flush; `sendBeacon` on page unload
- [ ] [P0] Private attribute stripping
- [ ] [P0] `sdk/ts/test/conformance.test.ts`: run **all** shared fixtures
- [ ] [P0] Package exports: ESM + CJS + `.d.ts`
- [ ] [P0] `bun test` all green; `bun run tsc --noEmit` zero errors

---

## Phase 8 — Analytics Pipeline (16 tasks)

- [ ] [P1] `internal/analytics/events.go`: `EvalEvent`, `TrackEvent`, `IdentifyEvent`, `SummaryEvent`, `FlagSummary`, `VariationCounter`
- [ ] [P1] `POST /sdk/events`: accept batched, gzip-encoded event payloads
- [ ] [P1] `internal/analytics/ingest.go`: buffered channel (4096) + N worker goroutines
- [ ] [P1] Batch writes: flush at 512 events or every 1 s, whichever first
- [ ] [P1] Dedupe window: `(contextKey, flagKey, variation, minute)` — SDK retries must not double-count
- [ ] [P1] Summary event expansion into per-variation counters
- [ ] [P1] `internal/analytics/store.go`: time-bucketed counters (minute / hour / day rollups)
- [ ] [P1] `internal/analytics/insights.go`: eval counts, variation distribution, unique contexts, over time
- [ ] [P1] Stale flag detection: no evaluations in N days **OR** 100% on one variation for N days
- [ ] [P1] `GET /projects/:proj/flags/:key/insights`
- [ ] [P1] `GET /projects/:proj/flags/stale`
- [ ] [P1] Backpressure: ingestion buffer full → return `429` with `Retry-After`, never block
- [ ] [P1] Unit test `TestIngest_Dedupe`: same event twice in one minute → counted once
- [ ] [P1] Unit test `TestIngest_SummaryExpansion`: summary with count=1000 → 1000 in the counter
- [ ] [P1] Unit test `TestStale_Detection`: flag at 100% for 31 days → flagged stale
- [ ] [P1] `go test ./internal/analytics/... -race` zero races

---

## Phase 9 — Statistics Engine (20 tasks)

- [ ] [P1] `internal/stats/distributions.go`: `normalCDF` via `math.Erfc` (stable in the tails)
- [ ] [P1] `normalQuantile` — Acklam's rational approximation, |error| < 1.15e-9
- [ ] [P1] `chiSquareCDF(x, df)` via the regularized lower incomplete gamma function
- [ ] [P1] `internal/stats/welford.go`: `RunningStats` with `Add`, `Variance` (Bessel-corrected), `StdDev`, `StdError`
- [ ] [P1] `RunningStats.Merge(other)`: parallel/partitioned aggregation
- [ ] [P1] `internal/stats/ztest.go`: `TwoProportionZTest(cn, cc, tn, tc, alpha)`
- [ ] [P1] **Pooled SE for the test statistic; UNPOOLED SE for the confidence interval** — using pooled for the CI is a classic subtle error
- [ ] [P1] Return absolute effect, relative effect (lift), z, two-sided p, CI bounds, significance
- [ ] [P1] `internal/stats/ttest.go`: Welch's t-test for numeric metrics (unequal variances)
- [ ] [P1] `internal/stats/msprt.go`: mixture SPRT → always-valid p-value + confidence sequence
- [ ] [P1] `tau` derived from the experiment's MDE (`tau = mde * 0.5` default)
- [ ] [P1] `internal/stats/srm.go`: `CheckSRM(observed, expectedWeights)` → χ², p-value, mismatch flag
- [ ] [P1] SRM threshold **0.001**, not 0.05 — false SRM alarms are very costly
- [ ] [P1] `internal/stats/samplesize.go`: `RequiredSampleSize(baseline, mde, alpha, power)`
- [ ] [P1] Unit test `TestZTest_KnownValues`: textbook example → z=2.42, p≈0.016
- [ ] [P1] Unit test `TestZTest_CIExcludesZeroWhenSignificant`: consistency between p<α and CI
- [ ] [P1] Unit test `TestWelford_MatchesNaive`: 10k samples → mean/variance match a two-pass computation to 1e-10
- [ ] [P1] Unit test `TestSRM_DetectsMismatch`: 52/48 of 100k → mismatch; 50.1/49.9 of 1k → no mismatch
- [ ] [P1] Unit test `TestMSPRT_ControlsTypeI`: 1000 simulated A/A tests with daily peeking → false-positive rate ≈ 5% (vs ≈25% for fixed-horizon)
- [ ] [P1] `go test ./internal/stats/... -race` zero races

---

## Phase 10 — Experiment Management (14 tasks)

- [ ] [P1] `internal/experiment/model.go`: `Experiment`, `Metric`, `MetricType`, `ExpStatus`, `AnalysisMethod`
- [ ] [P1] Experiment CRUD; link to a flag + environment + control/treatment variation indices
- [ ] [P1] `TrafficAllocation`: two-stage bucketing — first "in experiment?", then "which variation?"
- [ ] [P1] Starting an experiment sets `inExperiment: true` on the flag's rollout → SDKs emit individual eval events
- [ ] [P1] `internal/experiment/results.go`: join exposure events with conversion events on `contextKey`
- [ ] [P1] First-exposure-wins: a user's variation is fixed at their first exposure within the experiment window
- [ ] [P1] Conversion counted only if it occurs **after** first exposure
- [ ] [P1] Assemble per-variation `RunningStats` + `TwoProportionZTest` + `MSPRT` + `CheckSRM`
- [ ] [P1] Guardrail metrics evaluated separately; a guardrail regression flags the experiment even on a primary win
- [ ] [P1] Peek counter: increment on every results fetch; surfaced in the UI as a warning
- [ ] [P1] `GET /projects/:proj/experiments/:key/results`
- [ ] [P1] `POST /experiments/sample-size`
- [ ] [P1] Integration test `TestExperiment_DetectsTrueLift`: simulate 50k users with a real 2% lift → p < 0.05, CI excludes 0
- [ ] [P1] Integration test `TestExperiment_NoFalsePositiveOnAA`: A/A test with 50k users → p > 0.05

---

## Phase 11 — REST Gateway (20 tasks)

- [ ] [P0] `gateway/server.go`: Chi router, CORS, graceful shutdown, request logging
- [ ] [P0] Project & environment CRUD (6 endpoints incl. key rotation and env cloning)
- [ ] [P0] Flag CRUD (5 endpoints)
- [ ] [P0] `PUT /projects/:proj/flags/:key/environments/:env` — replace targeting config; validate before publish
- [ ] [P0] `POST .../toggle` — kill switch; **audit-logged with actor**
- [ ] [P0] `GET .../diff/:otherEnv` — config diff between environments
- [ ] [P0] Segment CRUD (4 endpoints)
- [ ] [P0] `POST /eval/:env/:flagKey` — server-side evaluation
- [ ] [P0] `POST /eval/:env` — evaluate all flags for one context
- [ ] [P0] `POST /eval/:env/:flagKey/explain` — full trace for the debugger
- [ ] [P0] `POST /eval/:env/:flagKey/simulate` — N synthetic contexts → variation histogram
- [ ] [P0] `GET /sdk/snapshot/:env` with ETag / `If-None-Match` → `304`
- [ ] [P0] `POST /sdk/events`
- [ ] [P0] `GET /sdk/bootstrap/:env` — pre-evaluated flags (client-side keys never receive rules)
- [ ] [P1] Experiment endpoints (6)
- [ ] [P1] `GET /projects/:proj/audit`
- [ ] [P1] `GET /ops/connections`, `GET /ops/metrics`
- [ ] [P1] `GET /scenarios`, `POST /scenarios/:name/run`
- [ ] [P0] **Admin SSE** `GET /api/v1/stream` — flag-changed / experiment-updated / connection events for the console
- [ ] [P0] Every mutation writes an audit entry with actor, before, after

---

## Phase 12 — Simulation & Load Generator (16 tasks)

- [ ] [P1] `cmd/loadgen/main.go`: spawn N synthetic SDK clients, each holding a real SSE connection
- [ ] [P1] Synthetic clients evaluate flags on a timer and emit realistic events
- [ ] [P1] `internal/simulation/scenarios.go`: 12 scenarios from SPEC §13
- [ ] [P1] `kill_switch`: toggle off; measure propagation to every client; assert p99 < 200 ms
- [ ] [P1] `gradual_rollout`: 1→5→25→50→100%; assert **monotonicity** (no user loses the feature)
- [ ] [P1] `targeting_rules`: one context × 6 rule configurations side by side
- [ ] [P1] `prerequisite_chain`: 3-level chain; break the root; all dependents fall back to off
- [ ] [P1] `sticky_bucketing`: same user × 1000 evaluations across restarts → identical variation every time
- [ ] [P1] `bucket_distribution`: 100k synthetic contexts → histogram vs configured weights + χ² test
- [ ] [P1] `sdk_parity`: same fixtures through both SDKs → assert identical results
- [ ] [P1] `reconnect_storm`: kill the hub; 500 clients reconnect; assert jittered spread (no thundering herd)
- [ ] [P1] `delta_vs_snapshot`: reconnect with stale `Last-Event-ID` → verify full-snapshot fallback path
- [ ] [P1] `experiment_run`: 50k users with a true 2% lift; p-value converges; fixed vs sequential compared
- [ ] [P1] `srm_injection`: deliberately skew assignment 52/48 → SRM detector fires
- [ ] [P1] `peeking_problem`: 100 A/A tests with daily peeking → show ≈25% fixed-horizon false-positive rate vs ≈5% mSPRT
- [ ] [P1] Chaos controls: kill hub, inject latency, drop a specific client, corrupt a delta

---

## Phase 13 — Frontend: Foundation + Flag List (16 tasks)

- [ ] [P0] `src/api/client.ts`: typed fetch wrapper with error normalization
- [ ] [P0] `src/api/queries.ts`: TanStack Query hooks — `useFlags`, `useFlag`, `useSegments`, `useExperiments`, `useConnections`, `useAudit`
- [ ] [P0] `src/api/schemas.ts`: zod schemas — **weights must sum to exactly 100000**, variation indices in range
- [ ] [P0] `src/sse/client.ts`: admin `EventSource` with reconnect; on flag-changed → `queryClient.invalidateQueries`
- [ ] [P0] `src/components/layout/`: shell with sidebar nav, environment switcher, project switcher
- [ ] [P0] **Environment switcher is global state** — every view is environment-scoped; production gets a red accent
- [ ] [P0] `src/components/flags/FlagList.tsx`: TanStack Table
- [ ] [P0] Columns: key, name, type, per-env on/off pills, variation count, tags, last evaluated, stale badge
- [ ] [P0] Global filter, column filters, sorting, pagination, column visibility
- [ ] [P0] Inline kill-switch toggle; **confirmation dialog required for production**
- [ ] [P1] Bulk select → tag / archive
- [ ] [P1] Stale flag callout banner
- [ ] [P0] `src/components/flags/CreateFlagDialog.tsx`: key, name, type, variations (dynamic list)
- [ ] [P0] Variation value editor switches on type: Switch (boolean), Input (string/number), Monaco (JSON)
- [ ] [P1] Live "flag key preview" with slug validation (lowercase, hyphens, unique)
- [ ] [P0] Optimistic updates with rollback on error

---

## Phase 14 — Frontend: Targeting Rule Builder ⭐ (24 tasks)

**The hardest and most important UI in the project.**

- [ ] [P0] `src/components/flags/FlagDetail.tsx`: environment tabs, master on/off, section layout mirroring evaluation order
- [ ] [P0] `src/components/targeting/PrerequisiteEditor.tsx`: flag combobox + required-variation select
- [ ] [P0] Prerequisite combobox excludes the current flag and any flag that would create a cycle
- [ ] [P0] `src/components/targeting/TargetEditor.tsx`: per-variation context-key chips with add/remove
- [ ] [P0] `src/components/targeting/RuleList.tsx`: **dnd-kit `SortableContext`** — drag to reorder
- [ ] [P0] Persistent "first match wins" hint; rule index badges update live during drag
- [ ] [P0] `src/components/targeting/RuleCard.tsx`: description, clause list, then variation-or-rollout
- [ ] [P0] `src/components/targeting/ClauseEditor.tsx`: attribute combobox + operator select + values input + negate toggle
- [ ] [P0] Attribute combobox suggests previously-seen attribute names from recent evaluation events
- [ ] [P0] **Operator list filters by inferred attribute type** — semver ops only for version-like attributes, date ops only for date-like
- [ ] [P0] Values input adapts: tag-chips for `in`, single field for comparisons, segment picker for `segmentMatch`
- [ ] [P0] `src/components/targeting/RolloutEditor.tsx`: one slider per variation
- [ ] [P0] **Sliders are linked**: dragging one redistributes the remainder proportionally across the others
- [ ] [P0] Numeric input alongside each slider (exact 100000-scale entry)
- [ ] [P0] Live sum indicator: green ✓ at exactly 100000, red ✗ otherwise; **Save disabled until valid**
- [ ] [P0] `bucketBy` attribute select (default `key`)
- [ ] [P0] `react-hook-form` `useFieldArray` nested two levels (rules → clauses) with zod resolver
- [ ] [P0] Dirty tracking: "3 unsaved changes" bar with Discard / Review diff / Save
- [ ] [P0] **Save opens a diff dialog first** — never silently apply a production targeting change
- [ ] [P0] Diff dialog renders a structural JSON diff (added/removed/changed)
- [ ] [P1] Keyboard shortcuts: `⌘S` save, `⌘Z` undo local edits
- [ ] [P1] "Copy targeting from environment ▾" action
- [ ] [P1] Rule duplicate / disable-without-delete
- [ ] [P0] Unsaved-changes navigation guard

---

## Phase 15 — Frontend: Views 3–8 (30 tasks)

**View 3 — Evaluation Debugger**
- [ ] [P0] `src/components/debugger/EvaluationDebugger.tsx`: split pane
- [ ] [P0] Left: Monaco JSON editor for the context, with schema validation
- [ ] [P0] Right: step-by-step trace rendered from `/explain` — every step, every clause, ✓/✗ per clause
- [ ] [P0] Result card: value, variation, reason object, bucket value when a rollout was used
- [ ] [P1] Saved test contexts (localStorage) with quick-switch
- [ ] [P1] "Why not variation X?" — explains what would have to change
- [ ] [P1] **Bulk simulate**: 10,000 synthetic contexts → variation histogram vs configured weights

**View 4 — Real-Time Propagation Monitor**
- [ ] [P0] `src/components/monitor/ConnectionList.tsx`: live SDK table — env, SDK+version, uptime, version lag, events
- [ ] [P0] `src/components/monitor/PropagationTimeline.tsx`: toggle a flag → horizontal bars fill as each SDK acks
- [ ] [P0] Target line at 200 ms; bars turn red if exceeded
- [ ] [P1] Latency histogram (p50/p95/p99) via Recharts
- [ ] [P1] Chaos control panel: kill hub, force reconnect storm, inject latency, drop a client

**View 5 — Experiments**
- [ ] [P1] `ExperimentList.tsx`: status, flag, metric, sample progress bar, lift, significance chip
- [ ] [P1] `ResultsPanel.tsx`: per-variation table — users, conversions, rate, lift, CI, p-value
- [ ] [P1] Conversion-rate-over-time chart with confidence bands (Recharts `Area` + `Line`)
- [ ] [P1] **Dual p-value display**: fixed-horizon (with peek-count warning) beside always-valid mSPRT
- [ ] [P1] **SRM banner** — red, blocking, non-dismissible when χ² p < 0.001
- [ ] [P1] Guardrail metrics section with regression highlighting
- [ ] [P1] `SampleSizeCalculator.tsx`: baseline + MDE + α + power → required n and estimated days

**View 6 — Segments**
- [ ] [P1] Segment list + detail reusing `ClauseEditor` from the rule builder
- [ ] [P1] Included / excluded key management with **"excluded wins" explanatory note**
- [ ] [P1] Live match estimate against sampled contexts

**View 7 — Audit Log & Diffs**
- [ ] [P1] `AuditLog.tsx`: chronological feed, filters (flag / actor / action / date range)
- [ ] [P1] `JsonDiff.tsx`: expandable structural diff, added green / removed red / changed amber
- [ ] [P1] One-click "revert to this version" with confirmation

**View 8 — Scenarios + Metrics**
- [ ] [P0] `ScenarioPanel.tsx`: scenario select, Run, speed control, live narration
- [ ] [P1] Metrics dashboard: evals/sec, propagation p50/p99, connected SDKs, snapshot size, delta ratio, ingest rate
- [ ] [P1] `bucket_distribution` scenario renders its histogram + χ² result inline
- [ ] [P1] `peeking_problem` scenario renders the false-positive-rate comparison chart

---

## Phase 16 — Integration Tests & Polish (18 tasks)

**Correctness**
- [ ] [P0] `TestE2E_PropagationUnder200ms`: 100 connected SDKs; toggle a flag; every SDK reflects it within 200 ms
- [ ] [P0] `TestE2E_SDKParity`: all conformance fixtures through Go and TS → identical results
- [ ] [P0] `TestE2E_StickyBucketing`: 10k users × 100 evaluations across SDK restarts → zero variation changes
- [ ] [P0] `TestE2E_RolloutMonotonicity`: ramp 10%→20%→50%; assert the enabled set only grows
- [ ] [P0] `TestE2E_ChecksumSelfHealing`: corrupt a delta in transit; SDK detects mismatch and refetches a full snapshot
- [ ] [P0] `TestE2E_ReconnectWithLastEventID`: disconnect, change 3 flags, reconnect → receives deltas, not a full snapshot
- [ ] [P0] `TestE2E_ReplayWindowExceeded`: disconnect, change 300 flags (ring capacity 256), reconnect → full snapshot fallback
- [ ] [P0] `TestE2E_SlowClientDisconnected`: one client stops reading; others keep receiving; slow one is disconnected
- [ ] [P1] `TestE2E_PrerequisiteChainBreak`: 3-level chain; toggle root off; all dependents serve off-variation
- [ ] [P1] `TestE2E_ExperimentTrueLift`: 50k users, 2% real lift → detected significant; A/A → not significant
- [ ] [P1] `TestE2E_SRMFires`: skewed assignment → SRM detector fires with p < 0.001
- [ ] [P0] `TestE2E_ServiceOutageDegradesGracefully`: kill the server mid-run; SDK keeps serving the last snapshot; zero errors thrown

**Quality**
- [ ] [P0] `go test ./... -race -count=3` zero data races
- [ ] [P0] `make conformance` green for both SDKs
- [ ] [P1] `golangci-lint run` zero errors
- [ ] [P1] `bun run tsc --noEmit` zero errors; TypeScript strict mode in both frontend and TS SDK
- [ ] [P2] `README.md`: architecture diagram, evaluation-order explainer, SDK quickstarts, all 8 views
- [ ] [P2] `EVALUATION.md` reviewed as a standalone normative document any third party could implement from

---

## Summary

| Phase | Tasks |
|-------|-------|
| 0. Bootstrap | 14 |
| 1. Data Model | 18 |
| 2. Evaluation Engine | 34 |
| 3. Conformance Suite | 16 |
| 4. Config Store & Snapshots | 20 |
| 5. SSE Streaming Hub | 22 |
| 6. Go SDK | 22 |
| 7. TypeScript SDK | 18 |
| 8. Analytics Pipeline | 16 |
| 9. Statistics Engine | 20 |
| 10. Experiment Management | 14 |
| 11. REST Gateway | 20 |
| 12. Simulation & Load Generator | 16 |
| 13. Frontend: Foundation + Flag List | 16 |
| 14. Frontend: Targeting Rule Builder | 24 |
| 15. Frontend: Views 3–8 | 30 |
| 16. Integration Tests & Polish | 18 |
| **TOTAL** | **338** |
