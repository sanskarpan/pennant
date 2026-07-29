package stats

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalCDF(t *testing.T) {
	assert.InDelta(t, 0.5, normalCDF(0), 1e-9)
	assert.InDelta(t, 0.9772, normalCDF(2.0), 1e-4)
	assert.InDelta(t, 0.0228, normalCDF(-2.0), 1e-4)
}

func TestNormalQuantile(t *testing.T) {
	assert.InDelta(t, 0.0, normalQuantile(0.5), 1e-9)
	assert.InDelta(t, 1.6449, normalQuantile(0.95), 1e-4)
	assert.InDelta(t, -1.6449, normalQuantile(0.05), 1e-4)
	// Round-trip: CDF(quantile(p)) ≈ p
	for _, p := range []float64{0.01, 0.1, 0.25, 0.5, 0.75, 0.9, 0.99} {
		q := normalQuantile(p)
		assert.InDelta(t, p, normalCDF(q), 1e-8, "round-trip failed for p=%f", p)
	}
}

func TestWelford_MatchesNaive(t *testing.T) {
	const n = 10000
	rng := rand.New(rand.NewSource(42))

	var rs RunningStats
	samples := make([]float64, n)
	for i := range samples {
		samples[i] = rng.Float64()*100 + 50
		rs.Add(samples[i])
	}

	// Naive two-pass mean
	sum := 0.0
	for _, s := range samples {
		sum += s
	}
	meanNaive := sum / float64(n)

	// Naive two-pass variance
	sumSq := 0.0
	for _, s := range samples {
		d := s - meanNaive
		sumSq += d * d
	}
	varNaive := sumSq / float64(n-1)

	assert.InDelta(t, meanNaive, rs.Mean, 1e-10, "mean mismatch")
	assert.InDelta(t, varNaive, rs.Variance(), 1e-10, "variance mismatch")
}

func TestWelford_Merge(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	var all RunningStats
	var a, b RunningStats
	for i := 0; i < 1000; i++ {
		x := rng.Float64()
		all.Add(x)
		if i < 500 {
			a.Add(x)
		} else {
			b.Add(x)
		}
	}
	a.Merge(b)
	assert.InDelta(t, all.Mean, a.Mean, 1e-10)
	assert.InDelta(t, all.Variance(), a.Variance(), 1e-10)
}

func TestZTest_KnownValues(t *testing.T) {
	// Textbook example: control 200/1000, treatment 230/1000
	result := TwoProportionZTest(1000, 200, 1000, 230, 0.05)
	assert.InDelta(t, 0.20, result.ControlRate, 1e-9)
	assert.InDelta(t, 0.23, result.TreatmentRate, 1e-9)
	assert.True(t, result.ZScore > 0, "z should be positive (treatment > control)")
	t.Logf("z=%.4f p=%.4f CI=[%.4f, %.4f]", result.ZScore, result.PValue, result.CILower, result.CIUpper)
}

func TestZTest_CIExcludesZeroWhenSignificant(t *testing.T) {
	// Large clear effect: treatment converts 2x more
	result := TwoProportionZTest(10000, 1000, 10000, 2000, 0.05)
	assert.True(t, result.Significant, "should be significant")
	assert.True(t, result.CILower > 0, "CI should exclude zero when significant (lower > 0)")
}

func TestZTest_CIIncludesZeroWhenNotSignificant(t *testing.T) {
	// Tiny difference
	result := TwoProportionZTest(100, 10, 100, 11, 0.05)
	assert.False(t, result.Significant, "should not be significant")
	assert.True(t, result.CILower < 0 && result.CIUpper > 0, "CI should include zero")
}

func TestSRM_DetectsMismatch(t *testing.T) {
	// 52/48 of 100k — clear mismatch
	result := CheckSRM([]int64{52000, 48000}, []float64{0.5, 0.5})
	assert.True(t, result.Mismatch, "52/48 of 100k should be detected as SRM")

	// 50.1/49.9 of 1k — no mismatch
	result2 := CheckSRM([]int64{501, 499}, []float64{0.5, 0.5})
	assert.False(t, result2.Mismatch, "small imbalance in small sample should not be SRM")
}

func TestMSPRT_ControlsTypeIErrorUnderPeeking(t *testing.T) {
	if testing.Short() {
		t.Skip("slow test")
	}
	const trials = 1000
	const usersPerDay = 500
	const days = 14
	const trueRate = 0.10

	fixedFalsePositives := 0
	seqFalsePositives := 0

	for trial := 0; trial < trials; trial++ {
		rng := rand.New(rand.NewSource(int64(trial)))
		var cn, cc, tn, tc int64
		fixedFired, seqFired := false, false

		for day := 0; day < days; day++ {
			for i := 0; i < usersPerDay; i++ {
				if rng.Intn(2) == 0 {
					cn++
					if rng.Float64() < trueRate {
						cc++
					}
				} else {
					tn++
					if rng.Float64() < trueRate {
						tc++
					}
				}
			}
			if !fixedFired && TwoProportionZTest(cn, cc, tn, tc, 0.05).Significant {
				fixedFired = true
			}
			tau := trueRate * 0.5
			if !seqFired && MSPRT(cn, cc, tn, tc, tau, 0.05).Decision == SeqRejectNull {
				seqFired = true
			}
		}
		if fixedFired {
			fixedFalsePositives++
		}
		if seqFired {
			seqFalsePositives++
		}
	}

	fixedRate := float64(fixedFalsePositives) / trials
	seqRate := float64(seqFalsePositives) / trials
	t.Logf("fixed-horizon false-positive rate under daily peeking: %.1f%%", fixedRate*100)
	t.Logf("mSPRT false-positive rate under daily peeking:         %.1f%%", seqRate*100)

	assert.Greater(t, fixedRate, 0.15, "peeking should inflate fixed-horizon Type I error above 5%%")
	assert.Less(t, seqRate, 0.08, "mSPRT should keep Type I error near the nominal 5%%")
}

func TestRequiredSampleSize(t *testing.T) {
	// Standard values: 10% baseline, 2% MDE, alpha=0.05, power=0.80
	n := RequiredSampleSize(0.10, 0.02, 0.05, 0.80)
	t.Logf("Required sample size: %d per arm", n)
	assert.Greater(t, n, int64(1000), "should need at least 1000 per arm")
	assert.Less(t, n, int64(100000), "should need less than 100k per arm for 2% MDE")
}

// ensure math import is used
var _ = math.Pi
