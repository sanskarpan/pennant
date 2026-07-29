package stats

import "math"

// SeqDecision is the sequential testing decision.
type SeqDecision string

const (
	SeqContinue    SeqDecision = "continue"
	SeqRejectNull  SeqDecision = "reject_null"
	SeqAcceptNull  SeqDecision = "accept_null"
)

// SequentialResult holds results of an mSPRT sequential test.
type SequentialResult struct {
	AlwaysValidPValue       float64
	ConfidenceSequenceLower float64
	ConfidenceSequenceUpper float64
	Decision                SeqDecision
	SamplesCollected        int64
	SamplesRequired         int64
}

// MSPRT implements the Mixture Sequential Probability Ratio Test.
// Gives "always-valid" p-values that remain correct regardless of peeking frequency.
// Reference: Johari, Pekelis, Walsh — "Always Valid Inference", Stanford/Optimizely
//
// tau is derived from the experiment's MDE: tau = MDE * 0.5 is a reasonable default.
func MSPRT(cn, cc, tn, tc int64, tau, alpha float64) SequentialResult {
	if cn == 0 || tn == 0 {
		return SequentialResult{Decision: SeqContinue, AlwaysValidPValue: 1.0}
	}

	pc := float64(cc) / float64(cn)
	pt := float64(tc) / float64(tn)
	delta := pt - pc

	v := pc*(1-pc)/float64(cn) + pt*(1-pt)/float64(tn)
	if v <= 0 {
		return SequentialResult{Decision: SeqContinue, AlwaysValidPValue: 1.0}
	}

	// Likelihood ratio under N(0, tau²) mixture prior on the true effect
	lr := math.Sqrt(v/(v+tau*tau)) *
		math.Exp((tau*tau*delta*delta)/(2*v*(v+tau*tau)))

	avP := math.Min(1.0, 1.0/lr)

	// Confidence sequence (invert the test)
	halfWidth := 0.0
	denom := tau * tau
	if denom > 0 {
		inner := v * (v + tau*tau) / denom * math.Log((v+tau*tau)/(v*alpha*alpha))
		if inner > 0 {
			halfWidth = math.Sqrt(inner)
		}
	}

	d := SeqContinue
	if avP < alpha {
		d = SeqRejectNull
	}

	return SequentialResult{
		AlwaysValidPValue:       avP,
		ConfidenceSequenceLower: delta - halfWidth,
		ConfidenceSequenceUpper: delta + halfWidth,
		Decision:                d,
		SamplesCollected:        cn + tn,
	}
}
