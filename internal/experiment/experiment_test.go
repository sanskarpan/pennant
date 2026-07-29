package experiment

import (
	"fmt"
	"testing"
	"time"

	"pennant/internal/analytics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExperimentStore_CRUD(t *testing.T) {
	store := NewMemExperimentStore()

	exp := &Experiment{
		Key:            "checkout-cta-test",
		Name:           "Checkout CTA Button Test",
		FlagKey:        "checkout-cta",
		EnvironmentKey: "production",
		ProjectKey:     "shop",
		Status:         StatusDraft,
		Alpha:          0.05,
		Power:          0.80,
		Variations: []ExperimentVariation{
			{VariationIndex: 0, Name: "Control", IsControl: true, Weight: 50000},
			{VariationIndex: 1, Name: "Treatment", IsControl: false, Weight: 50000},
		},
		Metrics: []Metric{
			{Key: "purchase", Name: "Purchase Rate", EventName: "purchase", Kind: "conversion", MDE: 0.02},
		},
	}

	require.NoError(t, store.CreateExperiment(exp))

	got, err := store.GetExperiment("checkout-cta-test")
	require.NoError(t, err)
	assert.Equal(t, "checkout-cta-test", got.Key)
	assert.Equal(t, StatusDraft, got.Status)

	exp.Status = StatusRunning
	now := time.Now()
	exp.StartedAt = &now
	require.NoError(t, store.UpdateExperiment(exp))

	got, err = store.GetExperiment("checkout-cta-test")
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, got.Status)

	list, err := store.ListExperiments("shop", "production")
	require.NoError(t, err)
	assert.Len(t, list, 1)

	require.NoError(t, store.DeleteExperiment("checkout-cta-test"))
	_, err = store.GetExperiment("checkout-cta-test")
	assert.Error(t, err)
}

func intPtr(i int) *int { return &i }

func TestEngine_ComputeResults(t *testing.T) {
	eventStore := analytics.NewMemEventStore()
	expStore := NewMemExperimentStore()
	engine := NewEngine(eventStore, expStore)

	now := time.Now()
	exp := &Experiment{
		Key:            "cta-test",
		FlagKey:        "cta-flag",
		EnvironmentKey: "prod",
		ProjectKey:     "myproject",
		Status:         StatusRunning,
		Alpha:          0.05,
		Power:          0.80,
		StartedAt:      &now,
		Variations: []ExperimentVariation{
			{VariationIndex: 0, Name: "Control", IsControl: true, Weight: 50000},
			{VariationIndex: 1, Name: "Treatment", Weight: 50000},
		},
		Metrics: []Metric{
			{Key: "signup", Name: "Signup Rate", EventName: "signup", Kind: "conversion", MDE: 0.02},
		},
	}

	// Seed events: 1000 control, 1000 treatment evals
	events := make([]*analytics.Event, 0, 2000)
	for i := 0; i < 1000; i++ {
		events = append(events, &analytics.Event{
			Kind:           analytics.EvalEvent,
			FlagKey:        "cta-flag",
			EnvironmentKey: "prod",
			ProjectKey:     "myproject",
			ContextKey:     fmt.Sprintf("ctrl-user-%d", i),
			VariationIndex: intPtr(0),
			ReasonKind:     "FALLTHROUGH",
			Timestamp:      now.UnixMilli(),
		})
		events = append(events, &analytics.Event{
			Kind:           analytics.EvalEvent,
			FlagKey:        "cta-flag",
			EnvironmentKey: "prod",
			ProjectKey:     "myproject",
			ContextKey:     fmt.Sprintf("treat-user-%d", i),
			VariationIndex: intPtr(1),
			ReasonKind:     "RULE_MATCH",
			Timestamp:      now.UnixMilli(),
		})
	}
	require.NoError(t, eventStore.InsertEvents(events))

	// Seed conversion events: 10% control converts, 20% treatment converts
	conversionEvents := make([]*analytics.Event, 0)
	for i := 0; i < 100; i++ {
		conversionEvents = append(conversionEvents, &analytics.Event{
			Kind:           analytics.CustomEvent,
			EnvironmentKey: "prod",
			ProjectKey:     "myproject",
			ContextKey:     fmt.Sprintf("ctrl-user-%d", i),
			MetricName:     "signup",
			MetricValue:    1,
			Timestamp:      now.UnixMilli(),
		})
	}
	for i := 0; i < 200; i++ {
		conversionEvents = append(conversionEvents, &analytics.Event{
			Kind:           analytics.CustomEvent,
			EnvironmentKey: "prod",
			ProjectKey:     "myproject",
			ContextKey:     fmt.Sprintf("treat-user-%d", i),
			MetricName:     "signup",
			MetricValue:    1,
			Timestamp:      now.UnixMilli(),
		})
	}
	require.NoError(t, eventStore.InsertEvents(conversionEvents))

	results, err := engine.ComputeResults(exp)
	require.NoError(t, err)
	assert.NotNil(t, results)
	assert.Equal(t, "cta-test", results.ExperimentKey)
	assert.Greater(t, results.TotalSamples, int64(0))
	assert.NotNil(t, results.SRMResult)
	assert.False(t, results.SRMResult.Mismatch, "50/50 split should not be SRM")
	require.Len(t, results.MetricResults, 1)
	assert.Len(t, results.MetricResults[0].Variants, 1)

	// Verify metric result has real data (not placeholder)
	variant := results.MetricResults[0].Variants[0]
	assert.Greater(t, variant.ControlConv, int64(0), "control conversions should be > 0")
	assert.Greater(t, variant.TreatmentConv, int64(0), "treatment conversions should be > 0")
	// Treatment converts at 2x rate → should be significant with n=1000 each
	assert.True(t, variant.Significant, "2x conversion rate should be statistically significant")
}
