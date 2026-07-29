# Pennant Feature Flag Service — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a LaunchDarkly-style feature flag service with Go backend, Go+TS SDKs, cross-language conformance suite, and a React admin console.

**Architecture:** Go control plane handles flag CRUD, SSE fan-out, analytics, and experiment stats. Both SDKs evaluate flags locally from an in-memory snapshot received over SSE, sharing a normative JSON conformance suite. React frontend is a dense admin console centered on a nested targeting rule builder.

**Tech Stack:** Go 1.22+, Chi router, SQLite (modernc), SHA-1 bucketing, SSE streaming; React 18 + Vite + shadcn/ui + TanStack Query/Table + dnd-kit + Recharts + Monaco; TypeScript SDK targeting browser + Node; Bun for frontend/TS tooling.

---

## Critical Path

Phase 0 → Phase 1 → **Phase 2 (GATE)** → Phase 3 (GATE) → Phases 4–11 (parallel) → Phase 12 → Phases 13–15 (frontend) → Phase 16

Phase 2 (eval engine) must be fully tested before anything else. Phase 3 (conformance) gates both SDKs.

---

## Phase 0 — Bootstrap

### Task 0.1: Go module + directory structure

**Files:**
- Create: `go.mod`
- Create: `config.yaml`
- Create: `Makefile`
- Create: `cmd/server/main.go` (stub)
- Create: `cmd/loadgen/main.go` (stub)
- Create: `internal/model/` (empty)
- Create: `internal/eval/` (empty)
- Create: `internal/store/` (empty)
- Create: `internal/snapshot/` (empty)
- Create: `internal/stream/` (empty)
- Create: `internal/sdkauth/` (empty)
- Create: `internal/analytics/` (empty)
- Create: `internal/stats/` (empty)
- Create: `internal/experiment/` (empty)
- Create: `internal/audit/` (empty)
- Create: `internal/simulation/` (empty)
- Create: `internal/events/` (empty)
- Create: `gateway/` (empty)
- Create: `sdk/go/` (empty)
- Create: `sdk/ts/` (empty)
- Create: `conformance/` (empty)
- Create: `test/` (empty)

- [ ] Run `go mod init pennant` in project root
- [ ] Run `go get github.com/go-chi/chi/v5 github.com/Masterminds/semver/v3 modernc.org/sqlite github.com/stretchr/testify/assert golang.org/x/sync/errgroup`
- [ ] Create all directories with `.gitkeep` files
- [ ] Write `config.yaml` with all defaults from SPEC §15
- [ ] Write `Makefile` with targets: `run`, `test`, `test-race`, `conformance`, `lint`, `frontend-dev`, `loadgen`
- [ ] Write stub `cmd/server/main.go`
- [ ] Commit: `chore: bootstrap go module and directory structure`

### Task 0.2: Frontend bootstrap

**Files:**
- Create: `frontend/` (entire Vite React TS project)
- Create: `frontend/vite.config.ts`
- Create: `frontend/src/api/types.ts`
- Create: `frontend/src/api/schemas.ts`
- Create: `frontend/src/App.tsx`

- [ ] Run `cd frontend && bun create vite . --template react-ts`
- [ ] Run `bun add` all deps per PROMPT.md Phase 0
- [ ] Run `bunx tailwindcss init -p && bunx shadcn@latest init`
- [ ] Add all shadcn components listed in CHECKLIST.md Phase 0
- [ ] Configure `vite.config.ts` with `/api` proxy to `http://localhost:8080`
- [ ] Commit: `chore: bootstrap frontend`

### Task 0.3: Internal events bus

**Files:**
- Create: `internal/events/bus.go`
- Create: `internal/events/bus_test.go`

- [ ] Write `Bus` with `Publish(topic, payload)`, `Subscribe(topic) <-chan any`, `Unsubscribe`
- [ ] Non-blocking publish: drop if subscriber buffer full
- [ ] Test: subscribe, publish, assert receive; unsubscribe, publish, assert no receive
- [ ] `go test ./internal/events/... -race`
- [ ] Commit: `feat: internal events bus`

---

## Phase 1 — Data Model

### Task 1.1: Flag model

**Files:**
- Create: `internal/model/flag.go`
- Create: `internal/model/flag_test.go`

- [ ] Write all types from SPEC §4.1: `VariationType`, `Variation`, `Flag`, `FlagConfig`, `Prerequisite`, `Target`, `Rule`, `VariationOrRollout`, `Rollout`, `WeightedVariation`
- [ ] Add JSON tags matching wire format exactly
- [ ] Implement `Flag.Validate()`: ≥2 variations, indices in range, weights sum 100000, no duplicate rule IDs
- [ ] Implement `Flag.ValidateVariationTypes()`: all values match declared `Type`
- [ ] Implement `Rule.Validate()`: ≥1 clause, exactly one of Variation/Rollout set
- [ ] Test `TestFlag_ValidateWeightSum`: 33334+33333+33333=100000 ✓; 33333×3 ✗
- [ ] Test `TestFlag_ValidateVariationIndices`: rule ref var 5 of 2-variation flag → error
- [ ] `go test ./internal/model/... -race`
- [ ] Commit: `feat: flag data model`

### Task 1.2: Clause, Segment, Context, Reason, Environment models

**Files:**
- Create: `internal/model/clause.go`
- Create: `internal/model/segment.go`
- Create: `internal/model/context.go`
- Create: `internal/model/reason.go`
- Create: `internal/model/environment.go`
- Create: `internal/model/model_test.go`

- [ ] Write `Clause` + all 14 `Operator` constants from SPEC §4.2
- [ ] Write `Segment`, `SegmentRule` from SPEC §4.3
- [ ] Write `Context` + `GetAttribute(name)` (built-ins shadow custom attrs) from SPEC §4.4
- [ ] Write `ReasonKind`, `ErrorKind`, `Reason` from SPEC §5.2
- [ ] Write `Project`, `Environment`, SDK key types
- [ ] Implement `Segment.Validate()`: no key in both Included and Excluded
- [ ] Test `TestContext_BuiltInAttributesShadow`: attr named "key" doesn't override `Context.Key`
- [ ] Test `TestSegment_Validate`: key in both lists → error
- [ ] `go test ./internal/model/... -race`
- [ ] Commit: `feat: clause/segment/context/reason/environment models`

---

## Phase 2 — Evaluation Engine (GATE — must be complete and fully tested)

### Task 2.1: Bucketing

**Files:**
- Create: `internal/eval/bucket.go`
- Create: `internal/eval/bucket_test.go`

- [ ] Write `ComputeBucket` exactly as in PROMPT.md §2.1
  - `BucketScale = 0xFFFFFFFFFFFFFFF` (2^60-1, NOT 2^59-1 from SPEC — PROMPT wins)
  - Hash input: seed != nil → `"{seed}.{id}"`, else `"{key}.{salt}.{id}"`
  - SHA-1, first 15 hex chars, parse int64, divide by BucketScale
- [ ] Write `stringifyBucketValue`: strings ✓, int ✓, int64 ✓, integral float64 ✓, non-integral float64 ✗, bool ✗
- [ ] Missing `bucketBy` attribute → return 0.0 (not an error)
- [ ] Write all 5 tests from PROMPT.md: `TestBucket_Deterministic`, `TestBucket_Uniform`, `TestBucket_IndependentPerFlag`, `TestBucket_Monotonic`, `TestBucket_KnownValues`
- [ ] Run tests once to capture actual values for `TestBucket_KnownValues`; fill in the hardcoded floats
- [ ] `go test ./internal/eval/... -race -count=3`
- [ ] Commit: `feat: deterministic SHA-1 bucketing`

### Task 2.2: Clause operators

**Files:**
- Create: `internal/eval/operators.go`
- Create: `internal/eval/operators_test.go`

- [ ] Write `matchOperator(op Operator, attrValue any, clauseValue json.RawMessage) bool`
- [ ] Implement all 14 operators:
  - `in`: exact equality, type-strict (string ≠ number)
  - `startsWith`, `endsWith`, `contains`: string only; non-string → false
  - `matches`: regex, compile-once cache (sync.Map), invalid regex → false, never panic
  - `lessThan`, `lessThanOrEqual`, `greaterThan`, `greaterThanOrEqual`: numeric only
  - `before`, `after`: RFC3339 or Unix-millis; parse failure → false
  - `semVerEqual`, `semVerLessThan`, `semVerGreaterThan`: via Masterminds/semver; unparseable → false
  - `segmentMatch`: handled in clauseMatches, NOT here
- [ ] Test: every operator × (match, no-match, wrong-type) = 42+ cases
- [ ] `go test ./internal/eval/... -race`
- [ ] Commit: `feat: all 14 clause operators`

### Task 2.3: Clause and segment matching

**Files:**
- Create: `internal/eval/clause.go`
- Create: `internal/eval/segment.go`
- Create: `internal/eval/clause_test.go`

- [ ] Write `clauseMatches` exactly as in PROMPT.md §2.3
  - segmentMatch: OR across values, return `!c.Negate` on first match
  - Missing attribute → `false` BEFORE negation (negation does NOT resurrect missing attrs)
  - Array attrs: OR across elements; OR across clause values
  - `Negate` inverts final result
- [ ] Write `ruleMatches`: AND across all clauses; empty clause list → false
- [ ] Write `segmentMatches`: Excluded wins over Included; segment rules with optional weight
- [ ] Test `TestClause_NegateMissingAttribute`: negated clause on missing attr → still no match
- [ ] Test `TestSegment_ExcludedBeatsIncluded`: key in both lists → not in segment
- [ ] `go test ./internal/eval/... -race`
- [ ] Commit: `feat: clause and segment matching`

### Task 2.4: Main evaluation algorithm

**Files:**
- Create: `internal/eval/evaluate.go`
- Create: `internal/eval/evaluate_test.go`

- [ ] Define `Store` interface: `GetFlag(key) (*model.Flag, *model.FlagConfig, bool)`, `GetSegment(key) (*model.Segment, bool)`
- [ ] Write `Evaluate(flag, cfg, ctx, store) (*int, any, Reason)` — SPEC §5 step order exactly:
  - Step 1: OFF check
  - Step 2: Prerequisites (depth guard 20, visited-set cycle detection → ErrPrereqCycle)
  - Step 3: Individual targets (checked BEFORE rules, always win)
  - Step 4: Rules (first match wins, in stored order)
  - Step 5: Fallthrough
- [ ] Write `resolveVariationOrRollout`: cumulative weight walk; floating-point safety net (last variation if rounds to 1.0)
- [ ] Write `offResult`, `variationResult` helpers
- [ ] Write comprehensive tests: off flag, missing prereq, prereq cycle, target match, rule match, fallthrough, rollout
- [ ] `go test ./internal/eval/... -race -count=3` — zero races
- [ ] Commit: `feat: main evaluation algorithm`

### Task 2.5: Explain (step-by-step trace)

**Files:**
- Create: `internal/eval/explain.go`

- [ ] Write `Trace`, `TraceStep` structs
- [ ] Write `Explain(flag, cfg, ctx, store) Trace` — every step, every clause result, for the debugger
- [ ] Commit: `feat: evaluation explain/trace`

---

## Phase 3 — Conformance Suite (GATE — gates both SDKs)

### Task 3.1: Fixture schema and directory structure

**Files:**
- Create: `conformance/schema.json`
- Create: `conformance/README.md`
- Create: `EVALUATION.md`

- [ ] Write JSON Schema for fixture format: `{name, flag, segments?, cases: [{context, expect: {variation, value, reason}}]}`
- [ ] Write EVALUATION.md as normative language-neutral spec (reproduce SPEC §5 with RFC-2119 MUST/SHOULD)
- [ ] Write conformance/README.md: how to add a fixture; the rule that fixtures are never edited once merged
- [ ] Commit: `docs: conformance schema and evaluation spec`

### Task 3.2: Off and target fixtures

**Files:**
- Create: `conformance/off/*.json` (8 files)
- Create: `conformance/targets/*.json` (12 files)

- [ ] `off/flag_off_no_off_variation.json`: flag off, no offVariation → nil variation
- [ ] `off/flag_off_with_off_variation.json`: flag off, offVariation=0 → variation 0
- [ ] `off/flag_archived.json`: archived flag treated as off
- [ ] 5 more off variants (on after off, prerequisites with off, etc.)
- [ ] 12 target fixtures: individual targeting, target precedence over rules
- [ ] Commit: `test: off and target conformance fixtures`

### Task 3.3: Clause fixtures

**Files:**
- Create: `conformance/clauses/*.json` (60 files)

- [ ] Write fixtures for all 14 operators × string/number/date/semver/array attributes × negation × missing attributes
- [ ] Key cases: missing attr negated → false; array attr OR semantics; segmentMatch
- [ ] Commit: `test: clause conformance fixtures`

### Task 3.4: Rules, rollout, segment, prerequisite, bucketing fixtures

**Files:**
- Create: `conformance/rules/*.json` (25 files)
- Create: `conformance/rollout/*.json` (30 files)
- Create: `conformance/segments/*.json` (25 files)
- Create: `conformance/prerequisites/*.json` (15 files)
- Create: `conformance/bucketing/*.json` (20 files with hardcoded bucket floats)
- Create: `conformance/canonical_json/*.json` (5 files)

- [ ] Rules: ordering (first-match-wins), multi-clause AND, rule→rollout
- [ ] Rollout: weight distribution, bucketBy, seed, integral-float keys, 0-weight vars
- [ ] Segments: included/excluded precedence, segment rules, weighted, segment-in-clause
- [ ] Prerequisites: pass, fail, prereq-off, nested chains, cycle detection
- [ ] Bucketing: run `ComputeBucket` for 20 known inputs; hardcode expected floats
- [ ] Commit: `test: rules/rollout/segment/prereq/bucketing fixtures`

### Task 3.5: Go SDK conformance test runner

**Files:**
- Create: `sdk/go/go.mod`
- Create: `sdk/go/conformance_test.go`
- Create: `sdk/go/fixture_store_test.go`

- [ ] Write `TestConformance` exactly as in PROMPT.md Phase 3
- [ ] Write `newFixtureStore(flag, segments)` helper implementing the `eval.Store` interface
- [ ] `go test ./sdk/go/... -run TestConformance` — must find fixtures and pass all cases
- [ ] Commit: `test: Go SDK conformance test runner`

### Task 3.6: TypeScript SDK setup + conformance test

**Files:**
- Create: `sdk/ts/package.json`
- Create: `sdk/ts/tsconfig.json`
- Create: `sdk/ts/src/types.ts`
- Create: `sdk/ts/src/bucket.ts`
- Create: `sdk/ts/src/operators.ts`
- Create: `sdk/ts/src/evaluate.ts`
- Create: `sdk/ts/test/conformance.test.ts`
- Create: `sdk/ts/test/helpers.ts`

- [ ] Write `sdk/ts/src/types.ts`: mirror all Go model types
- [ ] Write `sdk/ts/src/bucket.ts`: SHA-1 via node:crypto, **BigInt for 60-bit hash prefix**, stringifyBucketValue mirroring Go exactly
- [ ] Write `sdk/ts/src/operators.ts`: all 14 operators; semver hand-rolled
- [ ] Write `sdk/ts/src/evaluate.ts`: port SPEC §5 exactly — same step order, same edge cases
- [ ] Write `FixtureStore` helper in `test/helpers.ts`
- [ ] Write `conformance.test.ts` exactly as in PROMPT.md Phase 3
- [ ] `cd sdk/ts && bun test` — all pass
- [ ] Commit: `feat: TypeScript SDK evaluation engine + conformance tests`

---

## Phase 4 — Config Store & Snapshots

### Task 4.1: Store interface + in-memory implementation

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/memory.go`
- Create: `internal/store/memory_test.go`

- [ ] Define `ConfigStore` interface: CRUD for projects, environments, flags, segments; monotonic version counter; audit
- [ ] Implement in-memory store with `sync.RWMutex`
- [ ] Every write increments per-environment version counter (monotonic)
- [ ] Test: create flag, get flag, update flag, delete flag; verify version increments
- [ ] `go test ./internal/store/... -race`
- [ ] Commit: `feat: config store interface and in-memory implementation`

### Task 4.2: SQLite store

**Files:**
- Create: `internal/store/sqlite.go`
- Create: `internal/store/sqlite_test.go`

- [ ] Implement `ConfigStore` with `modernc.org/sqlite` (pure Go, no cgo)
- [ ] Schema: projects, environments, flags, segments, audit_entries tables
- [ ] Use `database/sql` transactions for atomic writes
- [ ] Test same CRUD suite as memory store
- [ ] `go test ./internal/store/... -race`
- [ ] Commit: `feat: SQLite config store`

### Task 4.3: Snapshot builder and canonical JSON

**Files:**
- Create: `internal/snapshot/builder.go`
- Create: `internal/snapshot/canonical.go`
- Create: `internal/snapshot/snapshot_test.go`

- [ ] Write `Snapshot`, `FlagConfigResolved` types
- [ ] Write `BuildSnapshot(store, envKey) *Snapshot`: resolve flags+segments for one environment
- [ ] Write canonical JSON: sorted keys, no whitespace, shortest round-trip numbers
- [ ] Write `Checksum(snapshot) string`: SHA-256 of canonical JSON, prefixed `sha256:`
- [ ] Test `TestCanonicalJSON_Stable`: same logical snapshot, different map iteration orders → identical bytes
- [ ] Test `TestChecksum_DetectsChange`: flip one bool → checksum changes
- [ ] `go test ./internal/snapshot/... -race`
- [ ] Commit: `feat: snapshot builder and canonical JSON checksumming`

### Task 4.4: Delta computation and publishing

**Files:**
- Create: `internal/snapshot/delta.go`
- Create: `internal/snapshot/publisher.go`

- [ ] Write `Delta` type matching SPEC §6
- [ ] Write `ComputeDelta(from, to *Snapshot) *Delta`: upserted + deleted flags/segments + resulting checksum
- [ ] Delta threshold: if delta size > 30% of snapshot size → publish full `put` instead of `patch`
- [ ] Write `SnapshotPublisher`: on any config write → rebuild snapshot → compute delta → publish to hub
- [ ] Publishing is atomic: readers always see complete snapshot
- [ ] Test `TestDelta_Roundtrip`: `apply(from, ComputeDelta(from,to))` == to, checksums match
- [ ] Test `TestDelta_ThresholdFallsBackToFull`: large change → publisher emits `put` not `patch`
- [ ] Commit: `feat: delta computation and snapshot publishing`

### Task 4.5: Audit log

**Files:**
- Create: `internal/audit/audit.go`
- Create: `internal/store/diff.go`

- [ ] Write `AuditEntry{Actor, Action, Resource, Before, After, At}`
- [ ] Record on every mutation via store hooks
- [ ] Write `internal/store/diff.go`: structural JSON diff for audit UI
- [ ] Commit: `feat: audit log and structural JSON diff`

---

## Phase 5 — SSE Streaming Hub

### Task 5.1: EventRing (replay buffer)

**Files:**
- Create: `internal/stream/ring.go`
- Create: `internal/stream/ring_test.go`

- [ ] Write `EventRing`: fixed-capacity circular buffer, default 256
- [ ] `Append(msg)`, `Since(version) ([]Message, ok bool)`: ok=false if version evicted
- [ ] Test `TestRing_ReplayWindow`: request evicted version → ok=false
- [ ] Test `TestRing_ReplayDeltas`: request version N → exactly deltas N+1..max
- [ ] `go test ./internal/stream/... -race -count=5`
- [ ] Commit: `feat: SSE event ring replay buffer`

### Task 5.2: Hub

**Files:**
- Create: `internal/stream/hub.go`
- Create: `internal/stream/hub_test.go`

- [ ] Write `Subscriber{ID, EnvKey, SDKKey, Ch chan Message (cap 32), LastSent, ConnectedAt, UserAgent, SDKVersion}`
- [ ] Write `Hub` with `Subscribe`, `Unsubscribe`, `Publish`, `Replay`
- [ ] `Publish`: non-blocking `select { case ch <- msg: default: }`; slow clients → mark for disconnect, never block
- [ ] Connection metrics: active count per env, publish rate, slow-client disconnects
- [ ] Test `TestHub_NonBlockingPublish`: one subscriber never reads; publish 1000 msgs; others still receive all
- [ ] `go test ./internal/stream/... -race -count=5`
- [ ] Commit: `feat: SSE hub with non-blocking publish and slow-client disconnect`

### Task 5.3: SDK auth

**Files:**
- Create: `internal/sdkauth/auth.go`

- [ ] Write `ValidateSDKKey(key, envKey) bool`
- [ ] Server vs client key distinction; client keys must not access full rule payloads
- [ ] Commit: `feat: SDK key auth`

### Task 5.4: SSE handler

**Files:**
- Create: `gateway/sse.go`

- [ ] Write `HandleStream` exactly as in PROMPT.md Phase 5
- [ ] Headers: `text/event-stream`, `no-cache`, `keep-alive`, **`X-Accel-Buffering: no`**
- [ ] Flush after headers; flush after every event
- [ ] Parse `Last-Event-ID` → replay deltas or full `put`
- [ ] 25s heartbeat: write `: heartbeat\n\n` (SSE comment) and flush
- [ ] Exit cleanly on `r.Context().Done()`; always `Unsubscribe` via defer
- [ ] Commit: `feat: SSE HTTP handler`

---

## Phase 6 — Go SDK

### Task 6.1: Go SDK client

**Files:**
- Create: `sdk/go/config.go`
- Create: `sdk/go/client.go`
- Create: `sdk/go/store.go`

- [ ] Write `Config` struct from SPEC §8.1
- [ ] Write `Client` with `store *atomic.Pointer[Snapshot]` (lock-free hot path)
- [ ] `NewClient(cfg)`: start streamer + event processor; return immediately
- [ ] `WaitForInit(timeout) error`: blocks for first snapshot; on timeout client is usable but degraded
- [ ] Write all variation methods: `BoolVariation`, `StringVariation`, `IntVariation`, `Float64Variation`, `JSONVariation`
- [ ] Write `*VariationDetail` variants returning `Reason`
- [ ] `AllFlags(ctx)`: bootstrap payload
- [ ] `Track(eventKey, ctx, value, data)`
- [ ] Type mismatch → return caller's default with ErrWrongType, never panic
- [ ] Test `TestSDK_DegradedWhenNotReady`: no server → BoolVariation returns default, reason CLIENT_NOT_READY
- [ ] `go test ./sdk/go/... -race -count=3`
- [ ] Commit: `feat: Go SDK client with lock-free hot path`

### Task 6.2: Go SDK streaming

**Files:**
- Create: `sdk/go/stream.go`

- [ ] SSE client: parse `event:`/`id:`/`data:` frames, handle `: comment` heartbeats
- [ ] Track `Last-Event-ID`; send on reconnect
- [ ] Apply `put` (replace snapshot) and `patch` (apply delta); verify checksum after apply
- [ ] Checksum mismatch → drop Last-Event-ID, refetch full snapshot (self-healing)
- [ ] Exponential backoff with **full jitter** (base 1s, max 30s)
- [ ] Polling mode: GET /sdk/snapshot/:env with ETag support
- [ ] Offline mode: load snapshot from file
- [ ] Commit: `feat: Go SDK streaming with self-healing delta apply`

### Task 6.3: Go SDK events

**Files:**
- Create: `sdk/go/events.go`

- [ ] `EventProcessor`: summary counters for normal evals; individual records only for `inExperiment`
- [ ] Batched flush every 5s or at capacity; drop-oldest on overflow (never block caller)
- [ ] Private attribute stripping: Config.PrivateAttributes and Context.Private
- [ ] `Close()`: flush events, close stream, stop goroutines
- [ ] Commit: `feat: Go SDK event processor with private attribute stripping`

---

## Phase 7 — TypeScript SDK

### Task 7.1: TS SDK client

**Files:**
- Create: `sdk/ts/src/client.ts`
- Create: `sdk/ts/src/backoff.ts`
- Create: `sdk/ts/src/events.ts`

- [ ] Write `FeatureFlagClient` with same public surface as Go SDK
- [ ] `EventSource` for streaming in browser; fetch + ReadableStream for Node (custom auth headers)
- [ ] Wrap EventSource — its auto-reconnect isn't configurable enough; use custom jittered backoff
- [ ] Checksum verification after delta apply; mismatch → full refetch
- [ ] `bootstrap` config option: hydrate from SSR-injected snapshot with zero initial network wait
- [ ] Typed variation methods + `*Detail` variants
- [ ] `on('ready' | 'update' | 'error' | 'reconnecting')` event emitter
- [ ] Event buffering + batched flush; sendBeacon on page unload
- [ ] Private attribute stripping
- [ ] Package exports: ESM + CJS + `.d.ts`
- [ ] `bun test && bun run tsc --noEmit`
- [ ] Commit: `feat: TypeScript SDK client`

---

## Phase 8 — Analytics Pipeline

### Task 8.1: Event types and ingestor

**Files:**
- Create: `internal/analytics/events.go`
- Create: `internal/analytics/ingest.go`
- Create: `internal/analytics/store.go`
- Create: `internal/analytics/insights.go`
- Create: `internal/analytics/ingest_test.go`

- [ ] Write all event types from SPEC §10.1: `EvalEvent`, `TrackEvent`, `IdentifyEvent`, `SummaryEvent`, `FlagSummary`, `VariationCounter`
- [ ] Write `Ingestor` with buffered channel (4096) + N worker goroutines
- [ ] Batch writes: flush at 512 events or every 1s
- [ ] Dedupe window: `(contextKey, flagKey, variation, minute)` — SDK retries don't double-count
- [ ] Summary event expansion into per-variation counters
- [ ] Time-bucketed counters: minute/hour/day rollups
- [ ] Backpressure: buffer full → 429 with Retry-After
- [ ] Test `TestIngest_Dedupe`: same event twice in one minute → counted once
- [ ] Test `TestIngest_SummaryExpansion`: summary with count=1000 → 1000 in counter
- [ ] `go test ./internal/analytics/... -race`
- [ ] Commit: `feat: analytics ingestion pipeline`

### Task 8.2: Stale flag detection + insights

- [ ] `GET /projects/:proj/flags/:key/insights`: eval counts, variation distribution, over time
- [ ] `GET /projects/:proj/flags/stale`: no evals in N days OR 100% one variation for N days
- [ ] Test `TestStale_Detection`: flag at 100% for 31 days → flagged stale
- [ ] Commit: `feat: stale flag detection and insights`

---

## Phase 9 — Statistics Engine

### Task 9.1: Distributions

**Files:**
- Create: `internal/stats/distributions.go`

- [ ] `normalCDF(z float64) float64` via `math.Erfc` (stable in tails)
- [ ] `normalQuantile(p float64) float64`: Acklam's rational approximation, |err| < 1.15e-9
- [ ] `chiSquareCDF(x, df float64) float64` via regularized lower incomplete gamma
- [ ] Commit: `feat: statistical distribution functions`

### Task 9.2: Welford running stats

**Files:**
- Create: `internal/stats/welford.go`
- Create: `internal/stats/welford_test.go`

- [ ] Write `RunningStats{N, Mean, M2}` with `Add`, `Variance` (Bessel-corrected), `StdDev`, `StdError`
- [ ] Write `Merge(other)` for parallel aggregation
- [ ] Test `TestWelford_MatchesNaive`: 10k samples → mean/variance match two-pass to 1e-10
- [ ] Commit: `feat: Welford online statistics`

### Task 9.3: Z-test

**Files:**
- Create: `internal/stats/ztest.go`
- Create: `internal/stats/ztest_test.go`

- [ ] Write `TwoProportionZTest` exactly as in PROMPT.md Phase 9
- [ ] **Pooled SE for test statistic; UNPOOLED SE for CI** — classic subtle error trap
- [ ] Test `TestZTest_KnownValues`: textbook example → correct z, p
- [ ] Test `TestZTest_CIExcludesZeroWhenSignificant`: p<α ↔ CI excludes 0
- [ ] Commit: `feat: two-proportion z-test`

### Task 9.4: mSPRT + SRM + sample size

**Files:**
- Create: `internal/stats/msprt.go`
- Create: `internal/stats/srm.go`
- Create: `internal/stats/samplesize.go`
- Create: `internal/stats/msprt_test.go`
- Create: `internal/stats/srm_test.go`

- [ ] Write `MSPRT` from SPEC §11.4
- [ ] Write `CheckSRM` from SPEC §11.5; threshold 0.001 (strict)
- [ ] Write `RequiredSampleSize` from SPEC §11.6
- [ ] Test `TestMSPRT_ControlsTypeIErrorUnderPeeking` exactly as in PROMPT.md Phase 9
- [ ] Test `TestSRM_DetectsMismatch`: 52/48 of 100k → mismatch; 50.1/49.9 of 1k → no mismatch
- [ ] `go test ./internal/stats/... -race`
- [ ] Commit: `feat: mSPRT sequential testing and SRM detection`

---

## Phase 10 — Experiment Management

### Task 10.1: Experiment model and CRUD

**Files:**
- Create: `internal/experiment/model.go`
- Create: `internal/experiment/results.go`

- [ ] Write `Experiment`, `Metric`, `MetricType`, `ExpStatus`, `AnalysisMethod` from SPEC §11.1
- [ ] Two-stage bucketing: "in experiment?" then "which variation?"
- [ ] Starting experiment sets `inExperiment: true` on rollout
- [ ] First-exposure-wins; conversion counted only after first exposure
- [ ] Assemble per-variation stats: `RunningStats` + `TwoProportionZTest` + `MSPRT` + `CheckSRM`
- [ ] Guardrail metrics evaluated separately
- [ ] Peek counter: increment on every results fetch
- [ ] Commit: `feat: experiment management`

---

## Phase 11 — REST Gateway

### Task 11.1: Server setup + project/env endpoints

**Files:**
- Create: `gateway/server.go`
- Create: `gateway/rest.go`

- [ ] Chi router, CORS (origin from config), graceful shutdown, request logging
- [ ] Project CRUD (GET /projects, POST /projects)
- [ ] Environment CRUD + key rotation + env cloning from SPEC §12
- [ ] Commit: `feat: REST gateway server and project/env endpoints`

### Task 11.2: Flag endpoints

- [ ] Flag CRUD: GET list, POST create, GET detail, PATCH update, DELETE archive
- [ ] PUT /projects/:proj/flags/:key/environments/:env (replace targeting config; validate before publish)
- [ ] POST .../toggle (kill switch; audit-logged with actor)
- [ ] GET .../diff/:otherEnv
- [ ] Every mutation writes audit entry with actor, before, after
- [ ] Commit: `feat: flag REST endpoints`

### Task 11.3: Segment + eval + SDK endpoints

- [ ] Segment CRUD (4 endpoints)
- [ ] POST /eval/:env/:flagKey — server-side evaluation
- [ ] POST /eval/:env — evaluate all flags
- [ ] POST /eval/:env/:flagKey/explain — full trace
- [ ] POST /eval/:env/:flagKey/simulate — N synthetic contexts → histogram
- [ ] GET /sdk/snapshot/:env with ETag/304
- [ ] POST /sdk/events
- [ ] GET /sdk/bootstrap/:env (client-side keys never receive rules)
- [ ] GET /sdk/stream/:env (SSE, delegates to sse.go)
- [ ] Admin SSE GET /api/v1/stream
- [ ] Commit: `feat: segment, eval, SDK, admin SSE endpoints`

### Task 11.4: Analytics + ops + experiment endpoints

- [ ] Experiment CRUD + start/stop/results (6 endpoints)
- [ ] GET /projects/:proj/audit
- [ ] GET /ops/connections, GET /ops/metrics
- [ ] POST /experiments/sample-size
- [ ] Commit: `feat: analytics, ops, experiment endpoints`

### Task 11.5: cmd/server/main.go — wire everything

**Files:**
- Modify: `cmd/server/main.go`

- [ ] Load config.yaml, open store, build snapshot, start hub + gateway + analytics
- [ ] Graceful shutdown on SIGINT/SIGTERM
- [ ] `go build ./cmd/server/` must succeed
- [ ] Commit: `feat: server main — wire all components`

---

## Phase 12 — Load Generator + Scenarios

### Task 12.1: Load generator

**Files:**
- Create: `cmd/loadgen/main.go`

- [ ] Spawn N synthetic SDK clients, each holding real SSE connection
- [ ] Evaluate flags on timer, emit realistic events
- [ ] Commit: `feat: synthetic SDK load generator`

### Task 12.2: Simulation scenarios

**Files:**
- Create: `internal/simulation/scenarios.go`

- [ ] Implement all 12 scenarios from SPEC §13: kill_switch, gradual_rollout, targeting_rules, prerequisite_chain, sticky_bucketing, bucket_distribution, sdk_parity, reconnect_storm, delta_vs_snapshot, experiment_run, srm_injection, peeking_problem
- [ ] Commit: `feat: all 12 simulation scenarios`

---

## Phase 13 — Frontend Foundation + Flag List

### Task 13.1: API client + types + schemas

**Files:**
- Create: `frontend/src/api/types.ts`
- Create: `frontend/src/api/schemas.ts`
- Create: `frontend/src/api/client.ts`
- Create: `frontend/src/api/queries.ts`

- [ ] TypeScript mirror of all Go model types in `types.ts`
- [ ] Zod schemas from PROMPT.md (rollout weights sum to 100000, variation indices in range)
- [ ] Typed fetch wrapper with error normalization in `client.ts`
- [ ] TanStack Query hooks: `useFlags`, `useFlag`, `useSegments`, `useExperiments`, `useConnections`, `useAudit`
- [ ] Commit: `feat: frontend API layer`

### Task 13.2: Admin SSE client

**Files:**
- Create: `frontend/src/sse/client.ts`

- [ ] Admin EventSource with reconnect; on flag-changed → queryClient.invalidateQueries
- [ ] Commit: `feat: frontend admin SSE client`

### Task 13.3: App shell + layout

**Files:**
- Create: `frontend/src/components/layout/Shell.tsx`
- Create: `frontend/src/App.tsx`

- [ ] Shell with sidebar nav, environment switcher (global state, production gets red accent), project switcher
- [ ] 8 routes: flag list, flag detail, debugger, monitor, experiments, segments, audit, scenarios
- [ ] QueryClientProvider, SSE connection on mount
- [ ] Commit: `feat: app shell and routing`

### Task 13.4: Flag list view

**Files:**
- Create: `frontend/src/components/flags/FlagList.tsx`
- Create: `frontend/src/components/flags/CreateFlagDialog.tsx`

- [ ] TanStack Table: key, name, type, per-env on/off pills, variation count, tags, last evaluated, stale badge
- [ ] Global filter, column filters, sorting, pagination, column visibility
- [ ] Inline kill-switch toggle; confirmation dialog for production
- [ ] CreateFlagDialog: key, name, type, variations (dynamic list)
- [ ] Variation value editor: Switch (boolean), Input (string/number), Monaco (JSON)
- [ ] Optimistic updates with rollback on error
- [ ] Commit: `feat: flag list and create dialog`

---

## Phase 14 — Targeting Rule Builder

### Task 14.1: FlagDetail + PrerequisiteEditor + TargetEditor

**Files:**
- Create: `frontend/src/components/flags/FlagDetail.tsx`
- Create: `frontend/src/components/targeting/PrerequisiteEditor.tsx`
- Create: `frontend/src/components/targeting/TargetEditor.tsx`

- [ ] Environment tabs, master on/off, section layout mirroring evaluation order
- [ ] Prerequisite combobox excludes current flag and cycle-creating flags
- [ ] Per-variation context-key chips with add/remove
- [ ] Commit: `feat: flag detail with prereq and target editors`

### Task 14.2: Rule list (dnd-kit)

**Files:**
- Create: `frontend/src/components/targeting/RuleList.tsx`
- Create: `frontend/src/components/targeting/RuleCard.tsx`

- [ ] dnd-kit SortableContext — drag to reorder; order is semantic
- [ ] Persistent "first match wins" hint; rule index badges update live during drag
- [ ] Commit: `feat: drag-to-reorder rule list`

### Task 14.3: Clause editor

**Files:**
- Create: `frontend/src/components/targeting/ClauseEditor.tsx`

- [ ] Attribute combobox (suggests previously-seen attrs from eval events)
- [ ] Operator list filters by inferred attribute type
- [ ] Values input adapts: tag-chips for `in`, single field for comparisons, segment picker for `segmentMatch`
- [ ] Negate toggle
- [ ] Commit: `feat: clause editor`

### Task 14.4: Rollout editor

**Files:**
- Create: `frontend/src/components/targeting/RolloutEditor.tsx`

- [ ] Linked sliders: dragging one redistributes remainder proportionally (PROMPT.md Phase 9 code)
- [ ] Numeric input alongside each slider (exact 100000-scale entry)
- [ ] Live sum indicator: green ✓ at exactly 100000, red ✗ otherwise; Save disabled until valid
- [ ] bucketBy attribute select
- [ ] Commit: `feat: linked rollout sliders`

### Task 14.5: Form wiring + diff dialog + save

**Files:**
- Modify: `frontend/src/components/flags/FlagDetail.tsx`

- [ ] react-hook-form useFieldArray nested two levels (rules → clauses) with zod resolver
- [ ] Dirty tracking: "3 unsaved changes" bar with Discard / Review diff / Save
- [ ] Save opens diff dialog first (structural JSON diff)
- [ ] Unsaved-changes navigation guard
- [ ] Commit: `feat: targeting form wiring with diff dialog`

---

## Phase 15 — Frontend Views 3–8

### Task 15.1: Evaluation Debugger

**Files:**
- Create: `frontend/src/components/debugger/EvaluationDebugger.tsx`

- [ ] Split pane: Monaco JSON editor left, step-by-step trace right
- [ ] Result card: value, variation, reason, bucket value for rollouts
- [ ] Saved test contexts (localStorage)
- [ ] Commit: `feat: evaluation debugger view`

### Task 15.2: Propagation Monitor

**Files:**
- Create: `frontend/src/components/monitor/ConnectionList.tsx`
- Create: `frontend/src/components/monitor/PropagationTimeline.tsx`

- [ ] Live SDK table: env, SDK+version, uptime, version lag, events
- [ ] Propagation timeline: toggle flag → bars fill as each SDK acks; target line at 200ms; bars red if exceeded
- [ ] Commit: `feat: real-time propagation monitor`

### Task 15.3: Experiments view

**Files:**
- Create: `frontend/src/components/experiments/ExperimentList.tsx`
- Create: `frontend/src/components/experiments/ResultsPanel.tsx`

- [ ] Results table, conversion-over-time chart with confidence bands (Recharts)
- [ ] Dual p-value display: fixed-horizon (with peek-count warning) + always-valid mSPRT
- [ ] SRM banner: red, non-dismissible when p < 0.001
- [ ] Guardrail metrics, sample size calculator
- [ ] Commit: `feat: experiments view with dual p-values and SRM banner`

### Task 15.4: Segments, Audit, Scenarios views

**Files:**
- Create: `frontend/src/components/segments/`
- Create: `frontend/src/components/audit/AuditLog.tsx`
- Create: `frontend/src/components/audit/JsonDiff.tsx`
- Create: `frontend/src/components/scenarios/ScenarioPanel.tsx`

- [ ] Segment list + detail reusing ClauseEditor; included/excluded wins note; live match estimate
- [ ] Audit: chronological feed, filters; expandable JSON diff; one-click revert
- [ ] Scenarios: select, Run, speed control, live narration; metrics dashboard
- [ ] Commit: `feat: segments, audit, scenarios views`

---

## Phase 16 — Integration Tests + Polish

### Task 16.1: E2E propagation test

**Files:**
- Create: `test/integration/propagation_test.go`

- [ ] `TestE2E_PropagationUnder200ms` exactly as in PROMPT.md Integration Tests
- [ ] 100 connected SDKs; toggle flag; every SDK reflects within 200ms p99
- [ ] Commit: `test: E2E propagation under 200ms`

### Task 16.2: E2E checksum self-healing + outage test

**Files:**
- Create: `test/integration/self_healing_test.go`
- Create: `test/integration/outage_test.go`

- [ ] `TestE2E_ChecksumSelfHealing` exactly as in PROMPT.md
- [ ] `TestE2E_ServiceOutageDegradesGracefully` exactly as in PROMPT.md
- [ ] Commit: `test: E2E self-healing and graceful degradation`

### Task 16.3: Race detection + conformance CI gate

- [ ] `go test ./... -race -count=3` → zero races
- [ ] `make conformance` green for both SDKs
- [ ] `bun run tsc --noEmit` zero errors
- [ ] Commit: `chore: all tests green, race-free`

---

## Makefile Reference

```makefile
.PHONY: run test test-race conformance lint frontend-dev loadgen

run:
	go run ./cmd/server/

test:
	go test ./...

test-race:
	go test ./... -race -count=3

conformance:
	go test ./sdk/go/... -run TestConformance
	cd sdk/ts && bun test conformance.test.ts

lint:
	golangci-lint run

frontend-dev:
	cd frontend && bun run dev

loadgen:
	go run ./cmd/loadgen/ -clients=$(CLIENTS)
```
