# CLAUDE CODE PROMPT — Feature Flag Service with Real-Time Updates

## Project Mission

Build a LaunchDarkly-style feature flag service from scratch:

- **Backend: Go** — evaluation engine (14 clause operators, segments, prerequisites, deterministic bucketing), versioned config snapshots with checksums + deltas, SSE streaming hub with `Last-Event-ID` replay, analytics ingestion, and an experimentation stats engine (z-test, mSPRT always-valid p-values, SRM detection)
- **Two SDKs: Go + TypeScript** — both evaluate flags **locally**, sharing one normative spec and one JSON conformance suite that CI runs against both
- **Frontend: React + TypeScript + Vite + shadcn/ui + Tailwind + TanStack Query/Table + dnd-kit + Recharts + Monaco** — admin console centered on a nested targeting rule builder

**Read `flags-SPEC.md` and `flags-CHECKLIST.md` before writing any code.**

**Two rules override everything else:**
1. **Phase 2 (evaluation engine) must be complete and fully unit-tested before anything is built on top of it.** Every other component is plumbing around this function.
2. **Phase 3 (conformance suite) gates both SDKs.** If the Go SDK and the TS SDK disagree on a single fixture, the build fails. This is the property that makes the whole system trustworthy.

---

## Phase 0 — Bootstrap

```bash
go mod init pennant
go get github.com/go-chi/chi/v5 github.com/Masterminds/semver/v3 \
       modernc.org/sqlite github.com/stretchr/testify/assert \
       golang.org/x/sync/errgroup

cd frontend
bun create vite . --template react-ts
bun add @tanstack/react-query @tanstack/react-table react-hook-form zod \
        @hookform/resolvers @dnd-kit/core @dnd-kit/sortable @dnd-kit/utilities \
        recharts @monaco-editor/react react-router-dom clsx tailwind-merge \
        lucide-react date-fns
bun add -d tailwindcss postcss autoprefixer
bunx tailwindcss init -p
bunx shadcn@latest init
bunx shadcn@latest add button card dialog select input badge table tabs \
                        switch slider popover tooltip alert sheet \
                        dropdown-menu form separator skeleton command
```

---

## Phase 2 — Evaluation Engine

### 2.1 Bucketing — the most important function in the project

```go
// internal/eval/bucket.go
package eval

import (
    "crypto/sha1"
    "encoding/hex"
    "fmt"
    "math"
    "strconv"
)

// BucketScale = 2^60 - 1. We take 15 hex chars (60 bits) from the SHA-1 digest.
const BucketScale = 0xFFFFFFFFFFFFFFF

// ComputeBucket returns a deterministic value in [0.0, 1.0).
//
// FOUR PROPERTIES THAT MUST HOLD FOREVER:
//   1. DETERMINISTIC — same inputs → same output, in Go, in TypeScript, on any
//      machine, in any year. Locked by conformance/bucketing/*.json.
//   2. UNIFORM — over a large population, buckets spread evenly across [0,1).
//   3. INDEPENDENT PER FLAG — including flag.Key and flag.Salt in the hash means
//      a user unlucky in one 10% rollout is NOT systematically unlucky in others.
//      Without this, the same ~10% of users would be guinea pigs for everything.
//   4. MONOTONE UNDER GROWTH — the bucket is a stable number compared against a
//      threshold, so raising 10% → 20% only ADDS users. Nobody loses a feature
//      mid-rollout, which would be a terrible user experience.
//
// WHY SHA-1 AND NOT MURMURHASH: SHA-1 is in every language's stdlib with
// byte-identical output. MurmurHash3 has multiple incompatible variants
// (x86_32 vs x64_128) and seed conventions; matching Go and TypeScript
// bit-for-bit is a known source of parity bugs. SHA-1 costs ~1µs vs ~50ns,
// which is irrelevant against a 1ms evaluation budget. Correctness wins.
func ComputeBucket(ctx *Context, bucketBy, key, salt string, seed *int) float64 {
    if bucketBy == "" {
        bucketBy = "key"
    }

    idValue, ok := ctx.GetAttribute(bucketBy)
    if !ok {
        return 0.0 // missing bucketBy attribute → deterministic 0, never an error
    }

    idStr, ok := stringifyBucketValue(idValue)
    if !ok {
        return 0.0
    }

    var hashInput string
    if seed != nil {
        hashInput = fmt.Sprintf("%d.%s", *seed, idStr)
    } else {
        hashInput = fmt.Sprintf("%s.%s.%s", key, salt, idStr)
    }

    sum := sha1.Sum([]byte(hashInput))
    hexPrefix := hex.EncodeToString(sum[:])[:15] // 15 hex chars = 60 bits
    n, err := strconv.ParseInt(hexPrefix, 16, 64)
    if err != nil {
        return 0.0
    }
    return float64(n) / float64(BucketScale)
}

// stringifyBucketValue: ONLY strings and integers are valid bucketing values.
//
// CROSS-LANGUAGE PARITY TRAP: float and bool string representations differ
// between languages. Go's strconv.FormatFloat gives "1e+10" where JavaScript's
// String() gives "10000000000". Two SDKs would then hash different bytes and
// bucket the same user differently — a silent, near-undebuggable production bug.
// Integral floats are allowed (JS numbers are all floats, so a JS caller passing
// userId 12345 arrives as float64(12345) and must work).
func stringifyBucketValue(v any) (string, bool) {
    switch t := v.(type) {
    case string:
        return t, true
    case int:
        return strconv.Itoa(t), true
    case int64:
        return strconv.FormatInt(t, 10), true
    case float64:
        if t == math.Trunc(t) && math.Abs(t) < 1e15 {
            return strconv.FormatInt(int64(t), 10), true
        }
        return "", false // non-integral float: reject
    default:
        return "", false // bool, nil, map, slice: reject
    }
}
```

**Tests that must pass before anything else is written:**

```go
func TestBucket_Deterministic(t *testing.T) {
    ctx := &Context{Kind: "user", Key: "user-4821"}
    first := ComputeBucket(ctx, "key", "my-flag", "salt123", nil)
    for i := 0; i < 10000; i++ {
        assert.Equal(t, first, ComputeBucket(ctx, "key", "my-flag", "salt123", nil))
    }
}

func TestBucket_Uniform(t *testing.T) {
    const n, bins = 100000, 100
    counts := make([]int64, bins)
    for i := 0; i < n; i++ {
        ctx := &Context{Kind: "user", Key: fmt.Sprintf("user-%d", i)}
        b := ComputeBucket(ctx, "key", "my-flag", "salt", nil)
        idx := int(b * bins)
        if idx >= bins { idx = bins - 1 }
        counts[idx]++
    }
    expected := make([]float64, bins)
    for i := range expected { expected[i] = 1.0 / bins }
    srm := stats.CheckSRM(counts, expected)
    assert.Greater(t, srm.PValue, 0.01, "buckets must be uniform, chi2=%f", srm.ChiSquare)
}

func TestBucket_IndependentPerFlag(t *testing.T) {
    // A user in the bottom 10% for flag A must NOT be systematically in the
    // bottom 10% for flag B.
    var bothLow, aLow int
    for i := 0; i < 10000; i++ {
        ctx := &Context{Kind: "user", Key: fmt.Sprintf("user-%d", i)}
        a := ComputeBucket(ctx, "key", "flag-a", "salt-a", nil)
        b := ComputeBucket(ctx, "key", "flag-b", "salt-b", nil)
        if a < 0.1 { aLow++; if b < 0.1 { bothLow++ } }
    }
    // If independent, P(b<0.1 | a<0.1) ≈ 0.1
    ratio := float64(bothLow) / float64(aLow)
    assert.InDelta(t, 0.1, ratio, 0.03, "flag buckets must be independent")
}

func TestBucket_Monotonic(t *testing.T) {
    // Users enabled at 10% MUST all still be enabled at 20%.
    in10 := map[string]bool{}
    for i := 0; i < 20000; i++ {
        k := fmt.Sprintf("user-%d", i)
        b := ComputeBucket(&Context{Kind: "user", Key: k}, "key", "f", "s", nil)
        if b < 0.10 { in10[k] = true }
    }
    for k := range in10 {
        b := ComputeBucket(&Context{Kind: "user", Key: k}, "key", "f", "s", nil)
        assert.Less(t, b, 0.20, "user %s lost the feature when ramping 10%%→20%%", k)
    }
}

// LOCKS THE HASH FUNCTION PERMANENTLY. If this test ever fails, someone has
// changed the bucketing algorithm and every user in every deployment would be
// re-bucketed. These values are also mirrored in conformance/bucketing/*.json.
func TestBucket_KnownValues(t *testing.T) {
    cases := []struct{ key, flag, salt string; want float64 }{
        {"user-0001", "checkout-experiment", "a1b2c3", 0.0}, // fill in on first run
        {"user-0002", "checkout-experiment", "a1b2c3", 0.0},
        {"user-0003", "checkout-experiment", "a1b2c3", 0.0},
    }
    for _, c := range cases {
        got := ComputeBucket(&Context{Kind: "user", Key: c.key}, "key", c.flag, c.salt, nil)
        assert.InDelta(t, c.want, got, 1e-12)
    }
}
```

### 2.2 The evaluation algorithm — implement SPEC §5 exactly

```go
// internal/eval/evaluate.go

const MaxPrerequisiteDepth = 20

func Evaluate(flag *Flag, cfg *FlagConfig, ctx *Context, store Store) (*int, any, Reason) {
    return evaluateWithDepth(flag, cfg, ctx, store, 0, map[string]bool{})
}

func evaluateWithDepth(
    flag *Flag, cfg *FlagConfig, ctx *Context, store Store,
    depth int, visited map[string]bool,
) (*int, any, Reason) {

    // ---- STEP 1: OFF ----
    if !cfg.On {
        return offResult(flag, cfg, Reason{Kind: ReasonOff})
    }

    // ---- STEP 2: PREREQUISITES ----
    if depth > MaxPrerequisiteDepth || visited[flag.Key] {
        return offResult(flag, cfg, Reason{Kind: ReasonError, ErrorKind: ErrPrereqCycle})
    }
    visited[flag.Key] = true

    for _, p := range cfg.Prerequisites {
        pf, pcfg, ok := store.GetFlag(p.FlagKey)
        if !ok {
            return offResult(flag, cfg, Reason{
                Kind: ReasonPrerequisiteFailed, PrerequisiteKey: p.FlagKey,
            })
        }
        // A prerequisite that is OFF always fails, regardless of its off-variation.
        if !pcfg.On {
            return offResult(flag, cfg, Reason{
                Kind: ReasonPrerequisiteFailed, PrerequisiteKey: p.FlagKey,
            })
        }
        pIdx, _, _ := evaluateWithDepth(pf, pcfg, ctx, store, depth+1, visited)
        if pIdx == nil || *pIdx != p.Variation {
            return offResult(flag, cfg, Reason{
                Kind: ReasonPrerequisiteFailed, PrerequisiteKey: p.FlagKey,
            })
        }
    }

    // ---- STEP 3: INDIVIDUAL TARGETS (checked BEFORE rules, always win) ----
    for _, t := range cfg.Targets {
        for _, k := range t.ContextKeys {
            if k == ctx.Key {
                return variationResult(flag, t.Variation, Reason{Kind: ReasonTargetMatch})
            }
        }
    }

    // ---- STEP 4: RULES — FIRST MATCH WINS, IN STORED ORDER ----
    for i, rule := range cfg.Rules {
        if ruleMatches(&rule, ctx, store) {
            reason := Reason{Kind: ReasonRuleMatch, RuleIndex: &i, RuleID: rule.ID}
            return resolveVariationOrRollout(flag, cfg, rule.VariationOrRollout, ctx, reason)
        }
    }

    // ---- STEP 5: FALLTHROUGH ----
    return resolveVariationOrRollout(flag, cfg, cfg.Fallthrough, ctx, Reason{Kind: ReasonFallthrough})
}

func resolveVariationOrRollout(
    flag *Flag, cfg *FlagConfig, vr VariationOrRollout, ctx *Context, reason Reason,
) (*int, any, Reason) {
    if vr.Variation != nil {
        return variationResult(flag, *vr.Variation, reason)
    }
    if vr.Rollout == nil || len(vr.Rollout.Variations) == 0 {
        return nil, nil, Reason{Kind: ReasonError, ErrorKind: ErrMalformedFlag}
    }

    ro := vr.Rollout
    bucket := ComputeBucket(ctx, ro.BucketBy, flag.Key, cfg.Salt, ro.Seed)

    sum := 0.0
    for _, wv := range ro.Variations { // STORED ORDER — never sort
        sum += float64(wv.Weight) / 100000.0
        if bucket < sum {
            return variationResult(flag, wv.Variation, reason)
        }
    }

    // FLOATING-POINT SAFETY NET.
    // If weights sum to exactly 100000, `sum` should reach 1.0 and bucket < 1.0
    // always matches. But float accumulation can leave sum at 0.9999999999999999.
    // Falling through here must NOT be an error — return the last variation.
    last := ro.Variations[len(ro.Variations)-1]
    return variationResult(flag, last.Variation, reason)
}

func offResult(flag *Flag, cfg *FlagConfig, reason Reason) (*int, any, Reason) {
    if cfg.OffVariation == nil {
        return nil, nil, reason // caller substitutes its own default
    }
    return variationResult(flag, *cfg.OffVariation, reason)
}

func variationResult(flag *Flag, idx int, reason Reason) (*int, any, Reason) {
    if idx < 0 || idx >= len(flag.Variations) {
        return nil, nil, Reason{Kind: ReasonError, ErrorKind: ErrMalformedFlag}
    }
    var val any
    _ = json.Unmarshal(flag.Variations[idx].Value, &val)
    return &idx, val, reason
}
```

### 2.3 Clause matching — the negation trap

```go
// internal/eval/clause.go

func clauseMatches(c *Clause, ctx *Context, store Store) bool {
    if c.Op == OpSegmentMatch {
        for _, raw := range c.Values {
            var segKey string
            if json.Unmarshal(raw, &segKey) != nil { continue }
            if seg, ok := store.GetSegment(segKey); ok && segmentMatches(seg, ctx, store) {
                return !c.Negate
            }
        }
        return c.Negate
    }

    attrValue, found := ctx.GetAttribute(c.Attribute)
    if !found {
        // CRITICAL: a missing attribute is ALWAYS a non-match, even when negated.
        // `NOT (plan in [free])` must NOT match a context with no `plan` attribute —
        // otherwise every anonymous/partial context silently falls into negated
        // rules, which is almost never what the rule author intended.
        return false
    }

    // Array attributes: OR across elements
    if arr, ok := attrValue.([]any); ok {
        for _, elem := range arr {
            for _, v := range c.Values {
                if matchOperator(c.Op, elem, v) { return !c.Negate }
            }
        }
        return c.Negate
    }

    for _, v := range c.Values { // OR across clause values
        if matchOperator(c.Op, attrValue, v) { return !c.Negate }
    }
    return c.Negate
}

func ruleMatches(r *Rule, ctx *Context, store Store) bool {
    for i := range r.Clauses { // AND across clauses
        if !clauseMatches(&r.Clauses[i], ctx, store) { return false }
    }
    return len(r.Clauses) > 0
}

// segmentMatches: EXCLUDED ALWAYS WINS over INCLUDED.
func segmentMatches(seg *Segment, ctx *Context, store Store) bool {
    for _, k := range seg.Excluded { if k == ctx.Key { return false } }
    for _, k := range seg.Included { if k == ctx.Key { return true } }

    for _, rule := range seg.Rules {
        all := true
        for i := range rule.Clauses {
            if !clauseMatches(&rule.Clauses[i], ctx, store) { all = false; break }
        }
        if !all { continue }
        if rule.Weight == nil { return true }
        b := ComputeBucket(ctx, rule.BucketBy, seg.Key, seg.Salt, nil)
        return b < float64(*rule.Weight)/100000.0
    }
    return false
}
```

---

## Phase 3 — Conformance Suite

```go
// sdk/go/conformance_test.go

type Fixture struct {
    Name     string              `json:"name"`
    Flag     *model.Flag         `json:"flag"`
    Segments []*model.Segment    `json:"segments"`
    Cases    []FixtureCase       `json:"cases"`
}

type FixtureCase struct {
    Context *model.Context `json:"context"`
    Expect  struct {
        Variation *int          `json:"variation"`
        Value     any           `json:"value"`
        Reason    *model.Reason `json:"reason"`
    } `json:"expect"`
}

func TestConformance(t *testing.T) {
    var files []string
    filepath.WalkDir("../../conformance", func(p string, d fs.DirEntry, err error) error {
        if err == nil && !d.IsDir() && strings.HasSuffix(p, ".json") &&
           !strings.HasSuffix(p, "schema.json") {
            files = append(files, p)
        }
        return nil
    })
    require.NotEmpty(t, files, "conformance fixtures not found")

    for _, f := range files {
        t.Run(filepath.Base(f), func(t *testing.T) {
            data, err := os.ReadFile(f)
            require.NoError(t, err)
            var fx Fixture
            require.NoError(t, json.Unmarshal(data, &fx))

            store := newFixtureStore(fx.Flag, fx.Segments)
            cfg := fx.Flag.Environments["production"]

            for i, c := range fx.Cases {
                t.Run(fmt.Sprintf("case-%d/%s", i, c.Context.Key), func(t *testing.T) {
                    idx, val, reason := eval.Evaluate(fx.Flag, cfg, c.Context, store)
                    assert.Equal(t, c.Expect.Variation, idx, "variation index")
                    assert.EqualValues(t, c.Expect.Value, val, "value")
                    if c.Expect.Reason != nil {
                        assert.Equal(t, c.Expect.Reason.Kind, reason.Kind, "reason kind")
                        if c.Expect.Reason.RuleIndex != nil {
                            assert.Equal(t, c.Expect.Reason.RuleIndex, reason.RuleIndex)
                        }
                    }
                })
            }
        })
    }
}
```

```typescript
// sdk/ts/test/conformance.test.ts
import { describe, test, expect } from 'bun:test';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { evaluate } from '../src/evaluate';
import { FixtureStore } from './helpers';

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) return walk(p);
    return p.endsWith('.json') && !p.endsWith('schema.json') ? [p] : [];
  });
}

// EXACTLY the same fixtures the Go SDK runs.
const files = walk('../../conformance');

for (const file of files) {
  const fx = JSON.parse(readFileSync(file, 'utf-8'));
  describe(fx.name, () => {
    const store = new FixtureStore(fx.flag, fx.segments ?? []);
    const cfg = fx.flag.environments.production;

    fx.cases.forEach((c: any, i: number) => {
      test(`case-${i}/${c.context.key}`, () => {
        const { variation, value, reason } = evaluate(fx.flag, cfg, c.context, store);
        expect(variation).toEqual(c.expect.variation);
        expect(value).toEqual(c.expect.value);
        if (c.expect.reason) {
          expect(reason.kind).toEqual(c.expect.reason.kind);
          if (c.expect.reason.rule_index !== undefined) {
            expect(reason.ruleIndex).toEqual(c.expect.reason.rule_index);
          }
        }
      });
    });
  });
}
```

### The TypeScript bucketing parity trap

```typescript
// sdk/ts/src/bucket.ts
import { createHash } from 'node:crypto';

const BUCKET_SCALE = 0xfffffffffffffffn; // BigInt: 2^60 - 1

export function computeBucket(
  ctx: EvaluationContext, bucketBy: string, key: string, salt: string, seed?: number,
): number {
  const attr = getAttribute(ctx, bucketBy || 'key');
  if (attr === undefined) return 0.0;

  const idStr = stringifyBucketValue(attr);
  if (idStr === null) return 0.0;

  const input = seed !== undefined ? `${seed}.${idStr}` : `${key}.${salt}.${idStr}`;
  const digest = createHash('sha1').update(input, 'utf8').digest('hex');

  // WARNING: CRITICAL: 15 hex chars = 60 bits. JavaScript's Number is a float64 with
  // only 53 bits of integer precision, so parseInt(hex, 16) SILENTLY LOSES the
  // low 7 bits and produces different buckets than Go. BigInt is mandatory here.
  const n = BigInt('0x' + digest.slice(0, 15));

  // Convert to float only AFTER the division, using Number on the ratio pieces.
  return Number(n) / Number(BUCKET_SCALE);
}

// Must mirror Go's stringifyBucketValue exactly.
function stringifyBucketValue(v: unknown): string | null {
  if (typeof v === 'string') return v;
  if (typeof v === 'number') {
    if (Number.isInteger(v) && Math.abs(v) < 1e15) return String(v);
    return null; // non-integral number: reject (same as Go)
  }
  return null;    // boolean, null, object, array: reject
}
```

---

## Phase 5 — SSE Streaming Hub

### Non-blocking publish (a single slow client must never stall everyone)

```go
// internal/stream/hub.go

func (h *Hub) Publish(envKey string, msg Message) {
    h.ring(envKey).Append(msg)

    h.mu.RLock()
    subs := make([]*Subscriber, 0, len(h.byEnv[envKey]))
    for _, s := range h.byEnv[envKey] { subs = append(subs, s) }
    h.mu.RUnlock()

    var slow []string
    for _, s := range subs {
        select {
        case s.Ch <- msg:
            atomic.StoreInt64(&s.LastSent, msg.Version)
        default:
            // Buffer full. This client is not keeping up. Disconnecting it is
            // strictly better than blocking every other subscriber: it will
            // reconnect and receive a fresh full snapshot.
            slow = append(slow, s.ID)
        }
    }
    for _, id := range slow {
        h.metrics.SlowClientDisconnects.Add(1)
        h.Unsubscribe(envKey, id)
    }
}
```

### The SSE handler

```go
// gateway/sse.go

func (s *Server) HandleStream(w http.ResponseWriter, r *http.Request) {
    envKey := chi.URLParam(r, "envKey")
    sdkKey := extractBearer(r)
    if !s.auth.ValidateSDKKey(sdkKey, envKey) {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming unsupported", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    // WARNING: Without X-Accel-Buffering, nginx buffers the whole response and NOTHING
    // is delivered until the connection closes. This one header is the difference
    // between "real-time" and "completely broken behind a reverse proxy".
    w.Header().Set("X-Accel-Buffering", "no")
    w.WriteHeader(http.StatusOK)
    flusher.Flush() // flush headers immediately so the client's onopen fires

    sub := s.hub.Subscribe(envKey, sdkKey, r.UserAgent())
    defer s.hub.Unsubscribe(envKey, sub.ID)

    lastID := parseLastEventID(r.Header.Get("Last-Event-ID"))
    if deltas, ok := s.hub.Replay(envKey, lastID); ok && lastID > 0 {
        for _, d := range deltas { writeSSE(w, flusher, d) }
    } else {
        snap := s.snapshots.Current(envKey)
        writeSSE(w, flusher, Message{Event: "put", Version: snap.Version, Data: snap})
    }

    // 25s < the typical 30s proxy idle timeout.
    heartbeat := time.NewTicker(25 * time.Second)
    defer heartbeat.Stop()

    for {
        select {
        case <-r.Context().Done():
            return
        case msg := <-sub.Ch:
            writeSSE(w, flusher, msg)
        case <-heartbeat.C:
            fmt.Fprint(w, ": heartbeat\n\n") // SSE comment; ignored by EventSource
            flusher.Flush()
        }
    }
}

func writeSSE(w io.Writer, f http.Flusher, m Message) {
    data, err := json.Marshal(m.Data)
    if err != nil { return }
    fmt.Fprintf(w, "event: %s\nid: %d\ndata: %s\n\n", m.Event, m.Version, data)
    f.Flush()
}
```

---

## Phase 6 — Go SDK: lock-free hot path + self-healing deltas

```go
// sdk/go/client.go

// The hot path: no mutex, no network, minimal allocation.
// atomic.Pointer swap on update means readers never block and never see a
// partially-applied snapshot.
func (c *Client) BoolVariation(key string, ctx *Context, def bool) bool {
    v, _ := c.boolDetail(key, ctx, def)
    return v
}

func (c *Client) boolDetail(key string, ctx *Context, def bool) (bool, Reason) {
    snap := c.store.Load()
    if snap == nil {
        // Not initialized yet. Return the caller's default — NEVER block,
        // NEVER error. A flag service outage must not take down the app.
        return def, Reason{Kind: ReasonError, ErrorKind: ErrClientNotReady}
    }
    flag, cfg, ok := snap.GetFlag(key)
    if !ok {
        c.events.RecordEval(key, ctx, nil, def, Reason{Kind: ReasonError, ErrorKind: ErrFlagNotFound})
        return def, Reason{Kind: ReasonError, ErrorKind: ErrFlagNotFound}
    }
    idx, val, reason := eval.Evaluate(flag, cfg, ctx, snap)
    b, ok := val.(bool)
    if !ok {
        return def, Reason{Kind: ReasonError, ErrorKind: ErrWrongType}
    }
    if cfg.TrackEvents || reason.InExperiment {
        c.events.RecordEval(key, ctx, idx, b, reason)
    }
    return b, reason
}
```

### Self-healing delta application

```go
// sdk/go/stream.go

func (s *StreamProcessor) applyMessage(msg Message) error {
    switch msg.Event {
    case "put":
        var snap Snapshot
        if err := json.Unmarshal(msg.Data, &snap); err != nil { return err }
        s.client.store.Store(&snap)
        s.lastEventID = snap.Version
        s.client.markReady()
        return nil

    case "patch", "delete":
        var delta Delta
        if err := json.Unmarshal(msg.Data, &delta); err != nil { return err }

        cur := s.client.store.Load()
        if cur == nil || cur.Version != delta.FromVersion {
            // We're not at the expected base version. Refetch rather than guess.
            return s.refetchFullSnapshot()
        }

        next := cur.ApplyDelta(&delta)

        // THE SELF-HEALING PROPERTY: verify the checksum after applying.
        // Any delta bug, any dropped field, any encoding mismatch degrades to
        // "one extra full-snapshot fetch" instead of "wrong flag values silently
        // served in production". This turns a correctness bug into a bandwidth bug.
        if next.Checksum != delta.Checksum {
            s.log.Warn("checksum mismatch after delta apply; refetching",
                "expected", delta.Checksum, "got", next.Checksum)
            s.lastEventID = 0 // don't ask for a delta next time
            return s.refetchFullSnapshot()
        }

        s.client.store.Store(next)
        s.lastEventID = next.Version
        return nil
    }
    return nil
}
```

### Reconnect with full jitter

```go
func (s *StreamProcessor) Run(ctx context.Context) {
    backoff := &Backoff{Base: time.Second, Max: 30 * time.Second}
    for {
        if ctx.Err() != nil { return }
        err := s.connectAndStream(ctx)
        if ctx.Err() != nil { return }
        d := backoff.Next()
        s.log.Info("stream disconnected, reconnecting", "err", err, "in", d)
        select {
        case <-time.After(d):
        case <-ctx.Done(): return
        }
    }
}

func (b *Backoff) Next() time.Duration {
    exp := float64(b.Base) * math.Pow(2, float64(b.attempt))
    if exp > float64(b.Max) { exp = float64(b.Max) }
    b.attempt++
    // FULL JITTER. Without it, a server restart makes 10,000 SDKs reconnect
    // in the same millisecond, and the server falls over again immediately.
    return time.Duration(rand.Float64() * exp)
}
```

---

## Phase 9 — Statistics: the pooled/unpooled distinction

```go
// internal/stats/ztest.go

func TwoProportionZTest(cn, cc, tn, tc int64, alpha float64) ProportionResult {
    if cn == 0 || tn == 0 { return ProportionResult{PValue: 1.0} }

    pc := float64(cc) / float64(cn)
    pt := float64(tc) / float64(tn)
    diff := pt - pc

    // POOLED standard error — for the TEST STATISTIC only.
    // The null hypothesis is p_c == p_t, so under the null both groups share
    // one proportion, and pooling gives the correct variance estimate.
    pPool := float64(cc+tc) / float64(cn+tn)
    sePooled := math.Sqrt(pPool * (1 - pPool) * (1/float64(cn) + 1/float64(tn)))

    z := 0.0
    if sePooled > 0 { z = diff / sePooled }
    p := 2 * (1 - normalCDF(math.Abs(z)))

    // UNPOOLED standard error — for the CONFIDENCE INTERVAL.
    // WARNING: CLASSIC SUBTLE ERROR: reusing sePooled here. The CI is NOT computed
    // under the null hypothesis — it's an estimate of the true difference — so
    // each group must contribute its own variance. Using the pooled SE produces
    // intervals that are subtly wrong, and worse, can disagree with the p-value
    // (p < 0.05 but the CI contains zero).
    seUnpooled := math.Sqrt(pc*(1-pc)/float64(cn) + pt*(1-pt)/float64(tn))
    zCrit := normalQuantile(1 - alpha/2)

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

// normalCDF via erfc — stable all the way into the tails. A naive Taylor or
// polynomial series loses all precision past |z| ≈ 6, which matters because
// large experiments routinely produce z-scores of 8+.
func normalCDF(z float64) float64 { return 0.5 * math.Erfc(-z/math.Sqrt2) }
```

**The test that proves the peeking problem is real:**

```go
func TestMSPRT_ControlsTypeIErrorUnderPeeking(t *testing.T) {
    const trials = 1000
    const usersPerDay = 500
    const days = 14
    const trueRate = 0.10 // A/A test: NO real difference

    fixedFalsePositives := 0
    seqFalsePositives := 0

    for trial := 0; trial < trials; trial++ {
        rng := rand.New(rand.NewSource(int64(trial)))
        var cn, cc, tn, tc int64
        fixedFired, seqFired := false, false

        for day := 0; day < days; day++ {
            for i := 0; i < usersPerDay; i++ {
                if rng.Intn(2) == 0 {
                    cn++; if rng.Float64() < trueRate { cc++ }
                } else {
                    tn++; if rng.Float64() < trueRate { tc++ }
                }
            }
            // Peek every day, as real product teams actually do
            if !fixedFired && TwoProportionZTest(cn, cc, tn, tc, 0.05).Significant {
                fixedFired = true
            }
            if !seqFired && MSPRT(cn, cc, tn, tc, 0.01, 0.05).Decision == SeqRejectNull {
                seqFired = true
            }
        }
        if fixedFired { fixedFalsePositives++ }
        if seqFired   { seqFalsePositives++ }
    }

    fixedRate := float64(fixedFalsePositives) / trials
    seqRate   := float64(seqFalsePositives) / trials
    t.Logf("fixed-horizon false-positive rate under daily peeking: %.1f%%", fixedRate*100)
    t.Logf("mSPRT false-positive rate under daily peeking:         %.1f%%", seqRate*100)

    assert.Greater(t, fixedRate, 0.15, "peeking should inflate fixed-horizon Type I error well above 5%%")
    assert.Less(t, seqRate, 0.08, "mSPRT should keep Type I error near the nominal 5%%")
}
```

---

## Frontend: the Targeting Rule Builder

### Linked rollout sliders (weights must sum to exactly 100000)

```typescript
// src/components/targeting/RolloutEditor.tsx

const SCALE = 100_000;

export function RolloutEditor({ variations, value, onChange }: Props) {
  const total = value.reduce((s, w) => s + w.weight, 0);
  const isValid = total === SCALE;

  // Dragging one slider redistributes the remainder PROPORTIONALLY across the
  // others, so the total always lands back on exactly 100000. Naively clamping
  // the dragged value leaves the user to fix the sum by hand, which is miserable.
  function handleChange(idx: number, newWeight: number) {
    const clamped = Math.max(0, Math.min(SCALE, Math.round(newWeight)));
    const remaining = SCALE - clamped;
    const othersTotal = value.reduce((s, w, i) => (i === idx ? s : s + w.weight), 0);

    const next = value.map((w, i) => {
      if (i === idx) return { ...w, weight: clamped };
      if (othersTotal === 0) {
        return { ...w, weight: Math.floor(remaining / (value.length - 1)) };
      }
      return { ...w, weight: Math.round((w.weight / othersTotal) * remaining) };
    });

    // Integer rounding can leave the sum 1–2 off. Push the residual onto the
    // last non-dragged variation so the invariant holds exactly.
    const drift = SCALE - next.reduce((s, w) => s + w.weight, 0);
    if (drift !== 0) {
      const target = next.findIndex((_, i) => i !== idx);
      if (target >= 0) next[target].weight += drift;
    }
    onChange(next);
  }

  return (
    <div className="space-y-3">
      {value.map((wv, i) => (
        <div key={wv.variation} className="flex items-center gap-3">
          <span className="w-28 truncate text-sm">{variations[wv.variation].name}</span>
          <Slider
            value={[wv.weight]} min={0} max={SCALE} step={100}
            onValueChange={([v]) => handleChange(i, v)}
            className="flex-1"
          />
          <Input
            type="number" className="w-24 text-right tabular-nums"
            value={wv.weight}
            onChange={(e) => handleChange(i, Number(e.target.value))}
          />
          <span className="w-16 text-right text-sm tabular-nums text-muted-foreground">
            {(wv.weight / SCALE * 100).toFixed(2)}%
          </span>
        </div>
      ))}
      <div className={cn('text-sm font-medium', isValid ? 'text-green-500' : 'text-red-500')}>
        Σ = {(total / SCALE * 100).toFixed(2)}% {isValid ? '[ok]' : `[fail] (must equal 100%)`}
      </div>
    </div>
  );
}
```

### Drag-to-reorder rules (order is semantic — first match wins)

```typescript
// src/components/targeting/RuleList.tsx
import { DndContext, closestCenter, type DragEndEvent } from '@dnd-kit/core';
import { SortableContext, arrayMove, verticalListSortingStrategy } from '@dnd-kit/sortable';

export function RuleList({ rules, onReorder, onUpdate, onDelete }: Props) {
  function handleDragEnd(e: DragEndEvent) {
    const { active, over } = e;
    if (!over || active.id === over.id) return;
    const from = rules.findIndex((r) => r.id === active.id);
    const to   = rules.findIndex((r) => r.id === over.id);
    // Reordering rules CHANGES EVALUATION RESULTS — this is a functional edit,
    // not a cosmetic one. The UI makes that consequence visible via index badges.
    onReorder(arrayMove(rules, from, to));
  }

  return (
    <div className="space-y-2">
      <Alert>
        <InfoIcon className="h-4 w-4" />
        <AlertDescription>
          Rules are evaluated top to bottom. <strong>The first matching rule wins.</strong>{' '}
          Drag to reorder.
        </AlertDescription>
      </Alert>
      <DndContext collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <SortableContext items={rules.map((r) => r.id)} strategy={verticalListSortingStrategy}>
          {rules.map((rule, i) => (
            <SortableRuleCard
              key={rule.id} rule={rule} index={i}
              onUpdate={(r) => onUpdate(i, r)} onDelete={() => onDelete(i)}
            />
          ))}
        </SortableContext>
      </DndContext>
    </div>
  );
}
```

### zod schema shared by the form and the API

```typescript
// src/api/schemas.ts
import { z } from 'zod';

const SCALE = 100_000;

export const weightedVariationSchema = z.object({
  variation: z.number().int().min(0),
  weight: z.number().int().min(0).max(SCALE),
});

export const rolloutSchema = z.object({
  variations: z.array(weightedVariationSchema).min(1),
  bucket_by: z.string().default('key'),
  seed: z.number().int().optional(),
}).refine(
  (r) => r.variations.reduce((s, v) => s + v.weight, 0) === SCALE,
  { message: 'Rollout weights must sum to exactly 100% (100000)', path: ['variations'] },
);

export const clauseSchema = z.object({
  attribute: z.string().min(1, 'Attribute is required'),
  op: z.enum([
    'in','endsWith','startsWith','matches','contains',
    'lessThan','lessThanOrEqual','greaterThan','greaterThanOrEqual',
    'before','after','semVerEqual','semVerLessThan','semVerGreaterThan','segmentMatch',
  ]),
  values: z.array(z.unknown()).min(1, 'At least one value is required'),
  negate: z.boolean().default(false),
});

export const ruleSchema = z.object({
  id: z.string(),
  description: z.string().optional(),
  clauses: z.array(clauseSchema).min(1, 'A rule needs at least one condition'),
  variation: z.number().int().optional(),
  rollout: rolloutSchema.optional(),
}).refine(
  (r) => (r.variation !== undefined) !== (r.rollout !== undefined),
  { message: 'A rule must serve either a fixed variation or a rollout, not both' },
);
```

---

## Integration Tests

### TestE2E_PropagationUnder200ms

```go
func TestE2E_PropagationUnder200ms(t *testing.T) {
    env := newTestEnv(t)
    defer env.Stop()

    const numClients = 100
    clients := make([]*sdk.Client, numClients)
    for i := range clients {
        c, err := sdk.NewClient(sdk.Config{
            SDKKey: env.SDKKey, StreamURL: env.StreamURL, Mode: sdk.Streaming,
        })
        require.NoError(t, err)
        require.NoError(t, c.WaitForInit(5*time.Second))
        clients[i] = c
        defer c.Close()
    }

    ctx := &sdk.Context{Kind: "user", Key: "user-1"}
    for _, c := range clients {
        require.False(t, c.BoolVariation("test-flag", ctx, false), "should start off")
    }

    start := time.Now()
    env.ToggleFlag("test-flag", "production", true)

    latencies := make([]time.Duration, numClients)
    var wg sync.WaitGroup
    for i, c := range clients {
        wg.Add(1)
        go func(i int, c *sdk.Client) {
            defer wg.Done()
            deadline := time.Now().Add(2 * time.Second)
            for time.Now().Before(deadline) {
                if c.BoolVariation("test-flag", ctx, false) {
                    latencies[i] = time.Since(start)
                    return
                }
                time.Sleep(time.Millisecond)
            }
            latencies[i] = 2 * time.Second
        }(i, c)
    }
    wg.Wait()

    sort.Slice(latencies, func(a, b int) bool { return latencies[a] < latencies[b] })
    p50 := latencies[numClients/2]
    p99 := latencies[numClients*99/100]
    t.Logf("propagation p50=%v p99=%v", p50, p99)
    assert.Less(t, p99, 200*time.Millisecond, "p99 propagation must be under 200ms")
}
```

### TestE2E_ChecksumSelfHealing

```go
func TestE2E_ChecksumSelfHealing(t *testing.T) {
    env := newTestEnvWithCorruptibleDeltas(t)
    defer env.Stop()

    c, _ := sdk.NewClient(sdk.Config{SDKKey: env.SDKKey, StreamURL: env.StreamURL})
    require.NoError(t, c.WaitForInit(5*time.Second))
    defer c.Close()

    ctx := &sdk.Context{Kind: "user", Key: "user-1"}

    // Corrupt the NEXT delta the server sends (drop one field)
    env.CorruptNextDelta()
    env.ToggleFlag("test-flag", "production", true)

    // The SDK must detect the checksum mismatch and refetch a full snapshot,
    // ending up with the CORRECT value — a delta bug must never produce a
    // wrong flag value, only extra bandwidth.
    require.Eventually(t, func() bool {
        return c.BoolVariation("test-flag", ctx, false)
    }, 5*time.Second, 10*time.Millisecond)

    assert.GreaterOrEqual(t, env.FullSnapshotRequests(), 2, "should have refetched after mismatch")
}
```

### TestE2E_ServiceOutageDegradesGracefully

```go
func TestE2E_ServiceOutageDegradesGracefully(t *testing.T) {
    env := newTestEnv(t)

    c, _ := sdk.NewClient(sdk.Config{SDKKey: env.SDKKey, StreamURL: env.StreamURL})
    require.NoError(t, c.WaitForInit(5*time.Second))
    defer c.Close()

    ctx := &sdk.Context{Kind: "user", Key: "user-1"}
    env.ToggleFlag("test-flag", "production", true)
    require.Eventually(t, func() bool { return c.BoolVariation("test-flag", ctx, false) },
        2*time.Second, 10*time.Millisecond)

    // Total outage
    env.Stop()
    time.Sleep(500 * time.Millisecond)

    // The SDK MUST keep serving the last known snapshot. No panics, no errors,
    // no blocking. A flag service outage must never take down the application.
    for i := 0; i < 1000; i++ {
        assert.True(t, c.BoolVariation("test-flag", ctx, false),
            "must keep serving last known value during outage")
    }
    _, reason := c.BoolVariationDetail("test-flag", ctx, false)
    assert.NotEqual(t, ReasonError, reason.Kind, "stale-but-valid data is not an error")
}
```

---

## Correctness Invariants to Verify

1. **Cross-SDK parity** — `make conformance` runs ~200 fixtures through Go and TS; zero divergence
2. **Bucketing locked** — `TestBucket_KnownValues` + `conformance/bucketing/*.json` pin the hash forever
3. **Rollout monotonicity** — `TestBucket_Monotonic`: ramping 10%→20% never removes a user
4. **Bucket independence** — `TestBucket_IndependentPerFlag`: no user is a permanent guinea pig
5. **Negation trap** — `TestClause_NegateMissingAttribute`: missing attributes never match, negated or not
6. **Excluded beats included** — `TestSegment_ExcludedBeatsIncluded`
7. **First match wins** — rule reordering changes results; covered by `conformance/rules/`
8. **Prerequisite cycles terminate** — depth guard + visited set → `PREREQUISITE_CYCLE`, never a hang
9. **Checksum self-healing** — `TestE2E_ChecksumSelfHealing`: delta bugs degrade to bandwidth, not wrong values
10. **Propagation < 200 ms p99** — `TestE2E_PropagationUnder200ms` with 100 clients
11. **Graceful degradation** — `TestE2E_ServiceOutageDegradesGracefully`: outage never throws
12. **Statistical validity** — pooled SE for the test statistic, unpooled for the CI; `TestMSPRT_ControlsTypeIErrorUnderPeeking` demonstrates the peeking problem quantitatively
13. **Race free** — `go test ./... -race -count=3`

---

## Code Standards

**Go**
- The evaluation function is **pure**: no I/O, no clock, no randomness. Same inputs → same outputs, always. This is what makes it testable via fixtures.
- `atomic.Pointer[Snapshot]` for the SDK store — the hot path never takes a lock
- `Hub.Publish` is non-blocking; a slow subscriber is disconnected, never waited on
- Every SSE handler sets `X-Accel-Buffering: no` and calls `Flush()` after the headers and after every event
- Never `panic` in SDK code. Every failure path returns the caller's default with an error reason.
- Snapshot publishing is atomic — readers see a complete snapshot at some version or the previous one, never a mix
- Statistics: `math.Erfc` for the normal CDF; Welford for variance; pooled SE for tests, unpooled for CIs

**TypeScript SDK**
- **`BigInt` for the 60-bit hash prefix.** `parseInt(hex, 16)` silently loses precision above 2^53 and will produce different buckets than Go. This is the single most likely parity bug.
- `stringifyBucketValue` must mirror Go exactly, including the integral-float rule
- Wrap `EventSource` — its built-in reconnect isn't configurable enough for jittered backoff
- Verify the checksum after every delta apply; on mismatch, drop `Last-Event-ID` and refetch

**Frontend**
- Environment is global state; production gets a visually distinct treatment and confirm-dialogs on writes
- Rollout weights are edited on the 0–100000 scale internally and displayed as percentages
- **Save always shows a diff first** for targeting changes — production flag edits are dangerous
- zod schemas are the single source of truth for validation, shared between forms and API calls
- TanStack Query cache invalidation is driven by the admin SSE stream, not polling

---

## Startup

```bash
# Terminal 1 — backend
make run

# Terminal 2 — frontend
cd frontend && bun run dev          # http://localhost:5173

# Terminal 3 — synthetic SDK fleet (for the propagation monitor)
make loadgen CLIENTS=100

# Terminal 4 — the gate that matters
make conformance                    # Go SDK + TS SDK, same fixtures, must agree
make test-race
```

Open `http://localhost:5173`.

**Run `kill_switch` first.** Open the Propagation Monitor (View 4) with 100 synthetic SDKs connected, then toggle a flag off in the Flag List. Watch 100 bars fill in green within 200 ms. That single interaction is the entire value proposition of a feature flag service — deploy decoupled from release, rollback in milliseconds instead of a 30-minute emergency deploy.

**Then run `sdk_parity`.** It pipes every conformance fixture through both SDKs and diffs the results. Watching ~200 fixtures pass in both Go and TypeScript is the moment the architecture proves itself: the evaluation algorithm is genuinely portable, and any future SDK in any language has an executable definition of correctness.

**Then run `peeking_problem`.** It runs 100 A/A tests — no real effect whatsoever — with daily peeking, and shows that the fixed-horizon p-value declares a false winner roughly **25%** of the time, while the always-valid mSPRT p-value stays near the nominal **5%**. This is the most under-appreciated result in the whole project: most teams running A/B tests are peeking daily at p < 0.05 and shipping noise as if it were signal.
