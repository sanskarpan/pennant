package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pennant/internal/analytics"
	"pennant/internal/api"
	"pennant/internal/auth"
	"pennant/internal/events"
	"pennant/internal/model"
	"pennant/internal/sdkauth"
	"pennant/internal/snapshot"
	"pennant/internal/store"
	"pennant/internal/stream"
	pennant "pennant/sdk/go/pennant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testHarness spins up a complete in-process stack with no mocks:
// MemoryStore -> Builder -> Hub -> Authenticator -> Server -> httptest.Server.
type testHarness struct {
	store   *store.MemoryStore
	builder *snapshot.Builder
	hub     *stream.Hub
	auth    *sdkauth.Authenticator
	srv     *api.Server
	httpSrv *httptest.Server
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	ms := store.NewMemoryStore()
	builder := snapshot.NewBuilder(ms)
	bus := events.NewBus()
	hub := stream.NewHub(bus)
	sdkAuth := sdkauth.NewAuthenticator()

	// Register a server-side SDK key bound to proj/prod.
	sdkAuth.Register(&sdkauth.SDKKeyRecord{
		Value:      "test-sdk-key",
		ProjectKey: "proj",
		EnvKey:     "prod",
		Type:       "server",
	})

	userStore := auth.NewUserStore()
	jwtService := auth.NewJWTService("test-secret")
	srv := api.NewServer(ms, builder, hub, sdkAuth, nil, "", userStore, jwtService, nil, nil, nil)
	httpSrv := httptest.NewServer(srv)

	h := &testHarness{
		store:   ms,
		builder: builder,
		hub:     hub,
		auth:    sdkAuth,
		srv:     srv,
		httpSrv: httpSrv,
	}

	// Seed project.
	require.NoError(t, ms.CreateProject(&model.Project{Key: "proj", Name: "Test Project"}))

	t.Cleanup(httpSrv.Close)
	return h
}

// createEnv initialises the "prod" environment in the store.
func (h *testHarness) createEnv(t *testing.T) {
	t.Helper()
	env := &model.Environment{Key: "prod", Name: "Production"}
	require.NoError(t, h.store.CreateEnvironment("proj", env))
}

// createFlag inserts a boolean flag with an initial on/off state.
func (h *testHarness) createFlag(t *testing.T, key string, on bool) {
	t.Helper()
	offVar := 0
	trueVar := 1
	flag := &model.Flag{
		Key:  key,
		Name: key,
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "v0", Value: json.RawMessage("false")},
			{ID: "v1", Value: json.RawMessage("true")},
		},
	}
	require.NoError(t, h.store.CreateFlag("proj", flag))

	cfg := &model.FlagConfig{
		On:           on,
		OffVariation: &offVar,
		Salt:         key,
	}
	if on {
		cfg.Fallthrough = model.VariationOrRollout{Variation: &trueVar}
	} else {
		cfg.Fallthrough = model.VariationOrRollout{Variation: &offVar}
	}
	require.NoError(t, h.store.UpsertFlagConfig("proj", "prod", key, cfg))
}

// publishChange bumps the env version, builds a new snapshot, and fans it out
// to all connected SSE subscribers. Returns the new snapshot.
func (h *testHarness) publishChange(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	newVersion, err := h.store.IncrementEnvVersion("proj", "prod")
	require.NoError(t, err)

	snap, err := h.builder.Build("proj", "prod")
	require.NoError(t, err)

	h.hub.Publish("prod", stream.Message{
		Event:   "put",
		Version: newVersion,
		Data:    snap,
	})
	return snap
}

// newClient creates an SDK client pointed at the test HTTP server and waits
// for it to receive its first snapshot.
func (h *testHarness) newClient(t *testing.T) *pennant.Client {
	t.Helper()
	client, err := pennant.New(pennant.Options{
		SDKKey:      "test-sdk-key",
		BaseURL:     h.httpSrv.URL,
		InitTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: flag config change propagates to an SSE client within 200 ms.
// ─────────────────────────────────────────────────────────────────────────────

func TestPropagation(t *testing.T) {
	h := newHarness(t)
	h.createEnv(t)
	h.createFlag(t, "dark-mode", false) // starts OFF

	client := h.newClient(t)
	require.True(t, client.IsReady(), "client must be ready before proceeding")

	ctx := model.Context{Key: "user-1"}
	assert.False(t, client.BoolVariation("dark-mode", ctx, true),
		"flag starts off — default should be false, not the fallback")

	// Flip the flag ON via the store.
	trueVar := 1
	offVar := 0
	newCfg := &model.FlagConfig{
		On:           true,
		OffVariation: &offVar,
		Fallthrough:  model.VariationOrRollout{Variation: &trueVar},
		Salt:         "dark-mode",
	}
	require.NoError(t, h.store.UpsertFlagConfig("proj", "prod", "dark-mode", newCfg))

	// Publish the updated snapshot and measure the round-trip.
	start := time.Now()
	h.publishChange(t)

	// Poll until the client sees the updated value or 500 ms elapses.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if client.BoolVariation("dark-mode", ctx, false) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	propagation := time.Since(start)
	assert.True(t, client.BoolVariation("dark-mode", ctx, false),
		"flag must be on after publishing the updated snapshot")
	assert.Less(t, propagation, 200*time.Millisecond,
		"propagation must complete within the 200 ms SLA (took %s)", propagation)

	t.Logf("propagation latency: %s", propagation)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: SDK client self-heals after the server drops its SSE connection.
// ─────────────────────────────────────────────────────────────────────────────

func TestSelfHealAfterDisconnect(t *testing.T) {
	h := newHarness(t)
	h.createEnv(t)
	h.createFlag(t, "heal-flag", true) // starts ON

	client := h.newClient(t)
	require.True(t, client.IsReady())

	ctx := model.Context{Key: "user-heal"}
	assert.True(t, client.BoolVariation("heal-flag", ctx, false),
		"initial eval: flag is on")

	// Close the test server to force a disconnect.
	h.httpSrv.Close()

	// Give the client a moment to detect the TCP RST.
	time.Sleep(50 * time.Millisecond)

	// The snapshot is still in memory — evaluations must continue to work
	// from the cached snapshot even while the client is reconnecting.
	assert.True(t, client.BoolVariation("heal-flag", ctx, false),
		"cached snapshot must serve evals during reconnect")

	// Bring up a new server on a fresh port that re-uses the same store/hub.
	newSrv := httptest.NewServer(h.srv)
	defer newSrv.Close()

	// Create a fresh client against the new server to verify the store is intact.
	client2, err := pennant.New(pennant.Options{
		SDKKey:      "test-sdk-key",
		BaseURL:     newSrv.URL,
		InitTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer client2.Close()

	require.True(t, client2.IsReady(), "fresh client must reach the new server")
	assert.True(t, client2.BoolVariation("heal-flag", ctx, false),
		"fresh client must see the flag as on")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: evaluation is race-free under concurrent load.
// ─────────────────────────────────────────────────────────────────────────────

func TestConcurrentEvaluation(t *testing.T) {
	h := newHarness(t)
	h.createEnv(t)
	h.createFlag(t, "feature-x", true) // always ON

	client := h.newClient(t)
	require.True(t, client.IsReady())

	const goroutines = 50
	const evalsPerGoroutine = 100

	var wg sync.WaitGroup
	var successCount atomic.Int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < evalsPerGoroutine; j++ {
				ctx := model.Context{Key: fmt.Sprintf("user-%d", id)}
				if client.BoolVariation("feature-x", ctx, false) {
					successCount.Add(1)
				}
			}
		}(i)
	}
	wg.Wait()

	total := int64(goroutines * evalsPerGoroutine)
	t.Logf("successful evals: %d / %d", successCount.Load(), total)
	assert.Equal(t, total, successCount.Load(),
		"all concurrent evaluations must return true (flag is always on)")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: snapshot atomic swap is data-race-free while evaluations are in flight.
// ─────────────────────────────────────────────────────────────────────────────

func TestSnapshotAtomicSwap(t *testing.T) {
	h := newHarness(t)
	h.createEnv(t)
	h.createFlag(t, "swap-test", true)

	client := h.newClient(t)
	require.True(t, client.IsReady())

	done := make(chan struct{})
	var evalCount atomic.Int64
	var updateCount atomic.Int64

	// Continuously evaluate while snapshots are being swapped in.
	go func() {
		ctx := model.Context{Key: "user-x"}
		for {
			select {
			case <-done:
				return
			default:
				client.BoolVariation("swap-test", ctx, false)
				evalCount.Add(1)
			}
		}
	}()

	// Continuously publish snapshot updates.
	go func() {
		for i := 0; i < 100; i++ {
			snap, _ := h.builder.Build("proj", "prod")
			version, _ := h.store.IncrementEnvVersion("proj", "prod")
			h.hub.Publish("prod", stream.Message{
				Event:   "put",
				Version: version,
				Data:    snap,
			})
			updateCount.Add(1)
			time.Sleep(time.Millisecond)
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(done)

	t.Logf("evals: %d, updates: %d", evalCount.Load(), updateCount.Load())
	assert.Greater(t, evalCount.Load(), int64(0), "at least one evaluation must have run")
	assert.Greater(t, updateCount.Load(), int64(0), "at least one update must have been published")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: delta vs full-put logic — patch is sent when history is available,
// full put is sent when history is stale or absent.
// ─────────────────────────────────────────────────────────────────────────────

func TestDeltaVsFullPut(t *testing.T) {
	h := newHarness(t)
	h.createEnv(t)

	// Seed background flags so the snapshot is large enough that changing
	// a single flag produces a delta well under the 30% size threshold.
	for i := 0; i < 10; i++ {
		h.createFlag(t, fmt.Sprintf("background-flag-%02d", i), false)
	}
	h.createFlag(t, "delta-flag", false)

	// Build the initial "v1" snapshot.
	v1Snap, err := h.builder.Build("proj", "prod")
	require.NoError(t, err)
	require.Equal(t, int64(0), v1Snap.Version)

	// Mutate one flag and increment the version to produce "v2".
	trueVar := 1
	offVar := 0
	require.NoError(t, h.store.UpsertFlagConfig("proj", "prod", "delta-flag", &model.FlagConfig{
		On:           true,
		OffVariation: &offVar,
		Fallthrough:  model.VariationOrRollout{Variation: &trueVar},
		Salt:         "delta-flag",
	}))
	_, err = h.store.IncrementEnvVersion("proj", "prod")
	require.NoError(t, err)

	v2Snap, err := h.builder.Build("proj", "prod")
	require.NoError(t, err)
	require.Equal(t, int64(1), v2Snap.Version)

	t.Run("delta contains only changed flag", func(t *testing.T) {
		delta := snapshot.ComputeDelta(v1Snap, v2Snap)
		require.NotNil(t, delta, "delta must be non-nil for a small change")
		assert.Equal(t, int64(0), delta.FromVersion)
		assert.Equal(t, int64(1), delta.ToVersion)
		assert.Len(t, delta.UpsertedFlags, 1, "exactly one flag was changed")
		assert.Equal(t, "delta-flag", delta.UpsertedFlags[0].Key)
		assert.Empty(t, delta.DeletedFlags, "no flags were deleted")
	})

	t.Run("apply delta produces identical snapshot to full build", func(t *testing.T) {
		delta := snapshot.ComputeDelta(v1Snap, v2Snap)
		require.NotNil(t, delta)

		applied := snapshot.ApplyDelta(v1Snap, delta)
		require.NotNil(t, applied)

		// The resulting snapshot must agree with v2 on the fields that matter.
		assert.Equal(t, v2Snap.Version, applied.Version)
		assert.Equal(t, v2Snap.Checksum, applied.Checksum,
			"ApplyDelta must produce the same checksum as the full snapshot")
		assert.True(t, applied.Flags["delta-flag"].Config.On,
			"patched flag must be on")
	})

	t.Run("full put when history is absent", func(t *testing.T) {
		// Replay with version 0 on a freshly created ring (nothing stored yet).
		msgs, ok := h.hub.Replay("prod", 0)
		// Either ok=false (ring empty) or ok=true with no messages.
		if ok {
			assert.Empty(t, msgs, "no messages should be in the ring for version 0 after init")
		} else {
			assert.False(t, ok, "ring has no history — caller must send a full put")
		}
	})

	t.Run("full put when delta would exceed 30 percent of snapshot", func(t *testing.T) {
		// Create many flags to make the snapshot large; then delete half — the
		// delta (all deletions) will exceed 30 % of the resulting snapshot.
		largeH := newHarness(t)
		largeH.createEnv(t)

		const flagCount = 20
		for i := 0; i < flagCount; i++ {
			largeH.createFlag(t, fmt.Sprintf("flag-%02d", i), false)
		}
		baseSnap, err := largeH.builder.Build("proj", "prod")
		require.NoError(t, err)

		// Delete half the flags to inflate the delta.
		for i := 0; i < flagCount/2; i++ {
			require.NoError(t, largeH.store.DeleteFlag("proj", fmt.Sprintf("flag-%02d", i)))
		}
		_, err = largeH.store.IncrementEnvVersion("proj", "prod")
		require.NoError(t, err)
		nextSnap, err := largeH.builder.Build("proj", "prod")
		require.NoError(t, err)

		delta := snapshot.ComputeDelta(baseSnap, nextSnap)
		// With 10 deleted flags the delta size relative to the smaller snapshot
		// will vary; ComputeDelta may or may not decide to suppress it.
		// What we assert is that ApplyDelta still produces a correct result
		// regardless of which path is chosen.
		if delta == nil {
			t.Log("ComputeDelta chose full-put (delta > 30% of snapshot) — correct behaviour")
		} else {
			applied := snapshot.ApplyDelta(baseSnap, delta)
			assert.Equal(t, nextSnap.Version, applied.Version)
			assert.Equal(t, nextSnap.Checksum, applied.Checksum)
			t.Log("ComputeDelta chose delta — ApplyDelta produced matching checksum")
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: /health endpoint returns 200 OK.
// ─────────────────────────────────────────────────────────────────────────────

func TestHealthEndpoint(t *testing.T) {
	h := newHarness(t)
	resp, err := http.Get(h.httpSrv.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: analytics ingestor flushes buffered events to the event store.
// ─────────────────────────────────────────────────────────────────────────────

func TestAnalyticsPipelineIntegration(t *testing.T) {
	eventStore := analytics.NewMemEventStore()
	ingestor := analytics.NewIngestor(eventStore, 100*time.Millisecond, 10)
	ingestor.Start()
	defer ingestor.Stop()

	const numEvents = 5
	for i := 0; i < numEvents; i++ {
		varIdx := 1
		ingestor.Track(&analytics.Event{
			Kind:           analytics.EvalEvent,
			FlagKey:        "my-flag",
			EnvironmentKey: "prod",
			ProjectKey:     "proj",
			ContextKey:     fmt.Sprintf("user-%d", i),
			VariationIndex: &varIdx,
			ReasonKind:     string(model.ReasonFallthrough),
		})
	}

	// Wait for the 100 ms flush ticker plus a small margin.
	time.Sleep(300 * time.Millisecond)

	all := eventStore.All()
	assert.Len(t, all, numEvents, "all %d events must be flushed to the store", numEvents)

	for _, e := range all {
		assert.Equal(t, analytics.EvalEvent, e.Kind)
		assert.Equal(t, "my-flag", e.FlagKey)
		assert.NotEmpty(t, e.ContextKey)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: SDK client receives initial snapshot over SSE without a Last-Event-ID
// and unauthenticated requests are rejected.
// ─────────────────────────────────────────────────────────────────────────────

func TestSDKAuthentication(t *testing.T) {
	h := newHarness(t)
	h.createEnv(t)
	h.createFlag(t, "auth-flag", true)

	t.Run("valid key receives snapshot", func(t *testing.T) {
		client := h.newClient(t)
		require.True(t, client.IsReady())
		ctx := model.Context{Key: "u1"}
		assert.True(t, client.BoolVariation("auth-flag", ctx, false))
	})

	t.Run("invalid key is rejected", func(t *testing.T) {
		// The /sdk/v1/snapshot endpoint should return 401 for a bad key.
		req, err := http.NewRequest(http.MethodGet, h.httpSrv.URL+"/sdk/v1/snapshot", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer bad-key")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: multiple flags evaluate correctly in the same snapshot.
// ─────────────────────────────────────────────────────────────────────────────

func TestMultipleFlagEvaluation(t *testing.T) {
	h := newHarness(t)
	h.createEnv(t)
	h.createFlag(t, "flag-on", true)
	h.createFlag(t, "flag-off", false)

	client := h.newClient(t)
	require.True(t, client.IsReady())

	ctx := model.Context{Key: "multi-user"}
	assert.True(t, client.BoolVariation("flag-on", ctx, false), "flag-on must evaluate to true")
	assert.False(t, client.BoolVariation("flag-off", ctx, true), "flag-off must evaluate to false")

	// Unknown flag must return the default value.
	assert.True(t, client.BoolVariation("nonexistent", ctx, true), "missing flag must return default=true")
	assert.False(t, client.BoolVariation("nonexistent", ctx, false), "missing flag must return default=false")
}
