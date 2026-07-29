package pennant_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	pennant "pennant/sdk/go/pennant"

	"pennant/internal/model"
	"pennant/internal/snapshot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestSnapshot(version int64, on bool) *snapshot.Snapshot {
	offVar := 0
	trueVar := 1
	cfg := &model.FlagConfig{
		On:           on,
		OffVariation: &offVar,
		Salt:         "test-flag",
	}
	if on {
		cfg.Fallthrough = model.VariationOrRollout{Variation: &trueVar}
	} else {
		cfg.Fallthrough = model.VariationOrRollout{Variation: &offVar}
	}
	snap := &snapshot.Snapshot{
		EnvironmentKey: "production",
		ProjectKey:     "default",
		Version:        version,
		Flags: map[string]*snapshot.ResolvedFlag{
			"test-flag": {
				Key:  "test-flag",
				Name: "Test Flag",
				Type: model.TypeBoolean,
				Variations: []model.Variation{
					{ID: "v0", Value: json.RawMessage("false")},
					{ID: "v1", Value: json.RawMessage("true")},
				},
				Config: cfg,
			},
		},
		Segments: make(map[string]*model.Segment),
	}
	checksum, _ := snapshot.Checksum(snap)
	snap.Checksum = checksum
	return snap
}

func serveSSE(t *testing.T, snap *snapshot.Snapshot) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")

		b, _ := json.Marshal(snap)
		fmt.Fprintf(w, "event: put\ndata: %s\n\n", b)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Hold connection open briefly then close.
		time.Sleep(50 * time.Millisecond)
	}))
}

func TestClient_InitAndEvaluate(t *testing.T) {
	snap := makeTestSnapshot(1, true)
	srv := serveSSE(t, snap)
	defer srv.Close()

	client, err := pennant.New(pennant.Options{
		SDKKey:      "sdk-key-123",
		BaseURL:     srv.URL,
		InitTimeout: 3 * time.Second,
	})
	require.NoError(t, err)
	defer client.Close()

	assert.True(t, client.IsReady())

	ctx := model.Context{Key: "user-1"}
	result := client.BoolVariation("test-flag", ctx, false)
	assert.True(t, result, "flag is on → should return true")
}

func TestClient_FlagOff(t *testing.T) {
	snap := makeTestSnapshot(1, false)
	srv := serveSSE(t, snap)
	defer srv.Close()

	client, err := pennant.New(pennant.Options{
		SDKKey:      "sdk-key-123",
		BaseURL:     srv.URL,
		InitTimeout: 3 * time.Second,
	})
	require.NoError(t, err)
	defer client.Close()

	ctx := model.Context{Key: "user-1"}
	result := client.BoolVariation("test-flag", ctx, true)
	assert.False(t, result, "flag is off → should return false (offVariation=0)")
}

func TestClient_DefaultOnMissingFlag(t *testing.T) {
	snap := makeTestSnapshot(1, true)
	srv := serveSSE(t, snap)
	defer srv.Close()

	client, err := pennant.New(pennant.Options{
		SDKKey:      "sdk-key-123",
		BaseURL:     srv.URL,
		InitTimeout: 3 * time.Second,
	})
	require.NoError(t, err)
	defer client.Close()

	ctx := model.Context{Key: "user-1"}
	result := client.BoolVariation("nonexistent-flag", ctx, true)
	assert.True(t, result, "missing flag → default value")
}

func TestClient_SelfHealing(t *testing.T) {
	var connectCount atomic.Int32
	snap := makeTestSnapshot(1, true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := connectCount.Add(1)
		if n == 1 {
			// First connection: send snapshot then close.
			w.Header().Set("Content-Type", "text/event-stream")
			b, _ := json.Marshal(snap)
			fmt.Fprintf(w, "event: put\ndata: %s\n\n", b)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(50 * time.Millisecond)
			return // close
		}
		// Subsequent connections: hold open.
		w.Header().Set("Content-Type", "text/event-stream")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	client, err := pennant.New(pennant.Options{
		SDKKey:      "sdk-key-123",
		BaseURL:     srv.URL,
		InitTimeout: 3 * time.Second,
	})
	require.NoError(t, err)
	defer client.Close()

	// Wait for at least one reconnect cycle.
	time.Sleep(2 * time.Second)
	assert.GreaterOrEqual(t, int(connectCount.Load()), 2, "client should have reconnected after disconnect")
	assert.True(t, client.IsReady(), "client should still be ready after reconnect")
}
