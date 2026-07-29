package experiment

import (
	"fmt"
	"time"

	"pennant/internal/analytics"
	"pennant/internal/stats"
)

// Engine computes experiment results from raw analytics events.
type Engine struct {
	eventStore analytics.EventStore
	expStore   ExperimentStore
}

func NewEngine(eventStore analytics.EventStore, expStore ExperimentStore) *Engine {
	return &Engine{eventStore: eventStore, expStore: expStore}
}

// ComputeResults runs statistical analysis for an experiment.
// It pulls eval events to get exposure counts and custom metric events for conversions.
func (e *Engine) ComputeResults(exp *Experiment) (*ExperimentResults, error) {
	// Query eval events for this flag since experiment started
	since := int64(0)
	if exp.StartedAt != nil {
		since = exp.StartedAt.UnixMilli()
	}

	insights, err := e.eventStore.QueryFlagInsights(exp.ProjectKey, exp.EnvironmentKey, since)
	if err != nil {
		return nil, err
	}

	// Find the insight for this flag
	var flagInsight *analytics.FlagInsight
	for _, ins := range insights {
		if ins.FlagKey == exp.FlagKey {
			flagInsight = ins
			break
		}
	}

	results := &ExperimentResults{
		ExperimentKey: exp.Key,
		ComputedAt:    time.Now(),
	}

	if flagInsight == nil {
		return results, nil
	}

	results.TotalSamples = flagInsight.TotalEvals

	// Build a map from variation index -> observed count for O(1) lookups.
	observedByVariation := make(map[int]int64, len(exp.Variations))
	for _, v := range exp.Variations {
		key := fmt.Sprintf("%d", v.VariationIndex)
		observedByVariation[v.VariationIndex] = flagInsight.VariationCounts[key]
	}

	// SRM check — pass counts and weights in variation order.
	observed := make([]int64, len(exp.Variations))
	expectedWeights := make([]float64, len(exp.Variations))
	for i, v := range exp.Variations {
		observed[i] = observedByVariation[v.VariationIndex]
		expectedWeights[i] = float64(v.Weight)
	}
	srmResult := stats.CheckSRM(observed, expectedWeights)
	results.SRMResult = &SRMSummary{
		ChiSquare: srmResult.ChiSquare,
		PValue:    srmResult.PValue,
		Mismatch:  srmResult.Mismatch,
	}

	// Compute per-metric results
	for _, metric := range exp.Metrics {
		mr := &MetricResult{
			MetricKey:  metric.Key,
			MetricName: metric.Name,
		}

		// Find the control variation.
		controlIdx := 0
		for _, v := range exp.Variations {
			if v.IsControl {
				controlIdx = v.VariationIndex
				break
			}
		}
		controlN := observedByVariation[controlIdx]

		// Query real conversion counts from analytics events
		conversions, err := e.eventStore.QueryConversions(
			exp.ProjectKey, exp.EnvironmentKey, exp.FlagKey, metric.EventName, since,
		)
		if err != nil {
			conversions = make(map[int]int64)
		}
		controlConv := conversions[controlIdx]

		for _, v := range exp.Variations {
			if v.IsControl {
				continue
			}
			treatN := observedByVariation[v.VariationIndex]
			treatConv := conversions[v.VariationIndex]

			if controlN == 0 || treatN == 0 {
				continue
			}

			alpha := exp.Alpha
			if alpha == 0 {
				alpha = 0.05
			}

			zResult := stats.TwoProportionZTest(controlN, controlConv, treatN, treatConv, alpha)
			tau := 0.02 // default tau for mSPRT; in production derived from metric.MDE
			seqResult := stats.MSPRT(controlN, controlConv, treatN, treatConv, tau, alpha)

			var samplesRequired int64
			if metric.MDE > 0 {
				baseline := float64(controlConv) / float64(controlN)
				samplesRequired = stats.RequiredSampleSize(baseline, metric.MDE, alpha, exp.Power)
			}

			vm := &VariantMetric{
				VariationIndex:    v.VariationIndex,
				Name:              v.Name,
				ControlN:          controlN,
				ControlConv:       controlConv,
				TreatmentN:        treatN,
				TreatmentConv:     treatConv,
				ControlRate:       zResult.ControlRate,
				TreatmentRate:     zResult.TreatmentRate,
				AbsoluteEffect:    zResult.AbsoluteEffect,
				RelativeEffect:    zResult.RelativeEffect,
				ZScore:            zResult.ZScore,
				PValue:            zResult.PValue,
				CILower:           zResult.CILower,
				CIUpper:           zResult.CIUpper,
				Significant:       zResult.Significant,
				AlwaysValidPValue: seqResult.AlwaysValidPValue,
				SeqDecision:       string(seqResult.Decision),
				SamplesRequired:   samplesRequired,
			}
			mr.Variants = append(mr.Variants, vm)
		}
		results.MetricResults = append(results.MetricResults, mr)
	}

	return results, nil
}
