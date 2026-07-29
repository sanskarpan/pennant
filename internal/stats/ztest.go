package stats

import "math"

// ProportionResult holds results of a two-proportion z-test.
type ProportionResult struct {
	ControlN, ControlConv     int64
	TreatmentN, TreatmentConv int64
	ControlRate, TreatmentRate float64
	AbsoluteEffect float64 // p_t - p_c
	RelativeEffect float64 // (p_t - p_c) / p_c (lift)
	ZScore         float64
	PValue         float64 // two-sided
	CILower        float64 // CI on the ABSOLUTE effect
	CIUpper        float64
	Significant    bool
}

// TwoProportionZTest performs a two-sided two-proportion z-test.
//
// IMPORTANT SUBTLETY:
// - Pooled SE is used for the TEST STATISTIC (null: p_c == p_t → shared proportion)
// - UNPOOLED SE is used for the CONFIDENCE INTERVAL (CI is not under the null)
// Using pooled SE for the CI is a classic error: it can produce p < α with CI containing 0.
func TwoProportionZTest(cn, cc, tn, tc int64, alpha float64) ProportionResult {
	if cn == 0 || tn == 0 {
		return ProportionResult{PValue: 1.0}
	}

	pc := float64(cc) / float64(cn)
	pt := float64(tc) / float64(tn)
	diff := pt - pc

	// Pooled proportion for the test statistic
	pPool := float64(cc+tc) / float64(cn+tn)
	sePooled := math.Sqrt(pPool * (1 - pPool) * (1/float64(cn) + 1/float64(tn)))

	z := 0.0
	if sePooled > 0 {
		z = diff / sePooled
	}
	p := 2 * (1 - normalCDF(math.Abs(z)))

	// Unpooled SE for the confidence interval
	seUnpooled := math.Sqrt(pc*(1-pc)/float64(cn) + pt*(1-pt)/float64(tn))
	zCrit := normalQuantile(1 - alpha/2)

	rel := 0.0
	if pc > 0 {
		rel = diff / pc
	}

	return ProportionResult{
		ControlN: cn, ControlConv: cc, TreatmentN: tn, TreatmentConv: tc,
		ControlRate: pc, TreatmentRate: pt,
		AbsoluteEffect: diff, RelativeEffect: rel,
		ZScore: z, PValue: p,
		CILower:     diff - zCrit*seUnpooled,
		CIUpper:     diff + zCrit*seUnpooled,
		Significant: p < alpha,
	}
}
