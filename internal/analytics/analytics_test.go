package analytics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intPtr(i int) *int { return &i }

func TestIngestor_FlushOnBatch(t *testing.T) {
	store := NewMemEventStore()
	ing := NewIngestor(store, 10*time.Second, 5) // flush at 5 events
	ing.Start()
	defer ing.Stop()

	for i := 0; i < 5; i++ {
		ing.Track(&Event{
			Kind:           EvalEvent,
			FlagKey:        "my-flag",
			EnvironmentKey: "prod",
			ProjectKey:     "proj",
			ContextKey:     "user-1",
			VariationIndex: intPtr(0),
		})
	}
	time.Sleep(50 * time.Millisecond)
	assert.Len(t, store.All(), 5, "5 events should have been flushed")
}

func TestIngestor_FlushOnInterval(t *testing.T) {
	store := NewMemEventStore()
	ing := NewIngestor(store, 100*time.Millisecond, 1000)
	ing.Start()
	defer ing.Stop()

	ing.Track(&Event{
		Kind:           EvalEvent,
		FlagKey:        "my-flag",
		EnvironmentKey: "prod",
		ProjectKey:     "proj",
		ContextKey:     "user-1",
		VariationIndex: intPtr(1),
	})

	time.Sleep(200 * time.Millisecond)
	assert.Len(t, store.All(), 1, "event should have been flushed on interval")
}

func TestMemStore_QueryFlagInsights(t *testing.T) {
	store := NewMemEventStore()
	now := time.Now().UnixMilli()

	events := []*Event{
		{Kind: EvalEvent, FlagKey: "flag-a", EnvironmentKey: "prod", ProjectKey: "proj", ContextKey: "u1", VariationIndex: intPtr(0), Timestamp: now, ReasonKind: "FALLTHROUGH"},
		{Kind: EvalEvent, FlagKey: "flag-a", EnvironmentKey: "prod", ProjectKey: "proj", ContextKey: "u2", VariationIndex: intPtr(1), Timestamp: now, ReasonKind: "RULE_MATCH"},
		{Kind: EvalEvent, FlagKey: "flag-a", EnvironmentKey: "prod", ProjectKey: "proj", ContextKey: "u1", VariationIndex: intPtr(0), Timestamp: now, ReasonKind: "FALLTHROUGH"},
		{Kind: EvalEvent, FlagKey: "flag-b", EnvironmentKey: "prod", ProjectKey: "proj", ContextKey: "u3", VariationIndex: intPtr(0), Timestamp: now, ReasonKind: "OFF"},
	}
	require.NoError(t, store.InsertEvents(events))

	insights, err := store.QueryFlagInsights("proj", "prod", 0)
	require.NoError(t, err)
	require.Len(t, insights, 2)

	byFlag := make(map[string]*FlagInsight)
	for _, ins := range insights {
		byFlag[ins.FlagKey] = ins
	}

	fA := byFlag["flag-a"]
	require.NotNil(t, fA)
	assert.Equal(t, int64(3), fA.TotalEvals)
	assert.Equal(t, int64(2), fA.UniqueContexts) // u1 and u2
	assert.Equal(t, int64(2), fA.VariationCounts["0"])
	assert.Equal(t, int64(1), fA.VariationCounts["1"])
}

func TestMemStore_QueryStaleFlags(t *testing.T) {
	store := NewMemEventStore()
	recentTime := time.Now().UnixMilli()
	oldTime := time.Now().Add(-31 * 24 * time.Hour).UnixMilli()

	events := []*Event{
		// recent flag — only one variation seen → single_variation stale
		{Kind: EvalEvent, FlagKey: "flag-boring", EnvironmentKey: "prod", ProjectKey: "proj", ContextKey: "u1", VariationIndex: intPtr(0), Timestamp: recentTime},
		// old flag — stale due to no recent evals
		{Kind: EvalEvent, FlagKey: "flag-old", EnvironmentKey: "prod", ProjectKey: "proj", ContextKey: "u1", VariationIndex: intPtr(0), Timestamp: oldTime},
		// healthy flag — recent and multiple variations
		{Kind: EvalEvent, FlagKey: "flag-healthy", EnvironmentKey: "prod", ProjectKey: "proj", ContextKey: "u1", VariationIndex: intPtr(0), Timestamp: recentTime},
		{Kind: EvalEvent, FlagKey: "flag-healthy", EnvironmentKey: "prod", ProjectKey: "proj", ContextKey: "u2", VariationIndex: intPtr(1), Timestamp: recentTime},
	}
	require.NoError(t, store.InsertEvents(events))

	stale, err := store.QueryStaleFlags("proj", "prod")
	require.NoError(t, err)

	staleByKey := make(map[string]*StaleFlag)
	for _, sf := range stale {
		staleByKey[sf.FlagKey] = sf
	}

	assert.NotNil(t, staleByKey["flag-boring"], "flag-boring should be stale (single variation)")
	assert.Equal(t, "single_variation", staleByKey["flag-boring"].Reason)
	assert.NotNil(t, staleByKey["flag-old"], "flag-old should be stale (no recent evals)")
	assert.Equal(t, "no_evals_30d", staleByKey["flag-old"].Reason)
	assert.Nil(t, staleByKey["flag-healthy"], "flag-healthy should NOT be stale")
}
