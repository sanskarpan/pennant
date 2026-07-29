package stats

import "math"

// RequiredSampleSize computes the required per-arm sample size for a two-proportion test.
// Formula: n = (z_{α/2} + z_β)² × [p_c(1-p_c) + p_t(1-p_t)] / (p_t - p_c)²
func RequiredSampleSize(baselineRate, mde, alpha, power float64) int64 {
	zAlpha := normalQuantile(1 - alpha/2)
	zBeta := normalQuantile(power)
	pc := baselineRate
	pt := baselineRate + mde
	num := math.Pow(zAlpha+zBeta, 2) * (pc*(1-pc) + pt*(1-pt))
	den := math.Pow(pt-pc, 2)
	if den == 0 {
		return 0
	}
	return int64(math.Ceil(num / den))
}
