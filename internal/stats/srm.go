package stats

// SRMResult holds the result of a Sample Ratio Mismatch chi-square test.
type SRMResult struct {
	Observed  []int64
	Expected  []float64
	ChiSquare float64
	PValue    float64
	// Mismatch is true when p < 0.001 — deliberately strict.
	// False SRM alarms are very costly (they invalidate whole experiments).
	Mismatch bool
}

// CheckSRM performs a chi-square goodness-of-fit test to detect sample ratio mismatch.
// observed: actual user counts per variation
// expectedWeights: relative weights (need not sum to 1; will be normalized)
func CheckSRM(observed []int64, expectedWeights []float64) SRMResult {
	var total int64
	for _, o := range observed {
		total += o
	}

	var weightSum float64
	for _, w := range expectedWeights {
		weightSum += w
	}

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
		Observed:  observed,
		Expected:  expected,
		ChiSquare: chi,
		PValue:    p,
		Mismatch:  p < 0.001,
	}
}
