package eval

import "pennant/internal/model"

type TraceStepKind string

const (
	TraceStepOff         TraceStepKind = "off_check"
	TraceStepPrereq      TraceStepKind = "prerequisite"
	TraceStepTarget      TraceStepKind = "target"
	TraceStepRule        TraceStepKind = "rule"
	TraceStepFallthrough TraceStepKind = "fallthrough"
)

type ClauseResult struct {
	Clause  model.Clause `json:"clause"`
	Matched bool         `json:"matched"`
}

type TraceStep struct {
	Kind          TraceStepKind  `json:"kind"`
	Matched       bool           `json:"matched"`
	RuleIndex     *int           `json:"rule_index,omitempty"`
	RuleID        string         `json:"rule_id,omitempty"`
	PrereqKey     string         `json:"prereq_key,omitempty"`
	ClauseResults []ClauseResult `json:"clause_results,omitempty"`
}

type Trace struct {
	Steps     []TraceStep  `json:"steps"`
	Variation *int         `json:"variation"`
	Value     any          `json:"value"`
	Reason    model.Reason `json:"reason"`
	Bucket    *float64     `json:"bucket,omitempty"`
}

func Explain(flag *model.Flag, cfg *model.FlagConfig, ctx *model.Context, store Store) Trace {
	var steps []TraceStep

	if !cfg.On {
		steps = append(steps, TraceStep{Kind: TraceStepOff, Matched: true})
		idx, val, reason := offResult(flag, cfg, model.Reason{Kind: model.ReasonOff})
		return Trace{Steps: steps, Variation: idx, Value: val, Reason: reason}
	}
	steps = append(steps, TraceStep{Kind: TraceStepOff, Matched: false})

	for _, p := range cfg.Prerequisites {
		pf, pcfg, ok := store.GetFlag(p.FlagKey)
		step := TraceStep{Kind: TraceStepPrereq, PrereqKey: p.FlagKey}
		if !ok || !pcfg.On {
			step.Matched = true
			steps = append(steps, step)
			idx, val, reason := offResult(flag, cfg, model.Reason{Kind: model.ReasonPrerequisiteFailed, PrerequisiteKey: p.FlagKey})
			return Trace{Steps: steps, Variation: idx, Value: val, Reason: reason}
		}
		pIdx, _, _ := Evaluate(pf, pcfg, ctx, store)
		if pIdx == nil || *pIdx != p.Variation {
			step.Matched = true
			steps = append(steps, step)
			idx, val, reason := offResult(flag, cfg, model.Reason{Kind: model.ReasonPrerequisiteFailed, PrerequisiteKey: p.FlagKey})
			return Trace{Steps: steps, Variation: idx, Value: val, Reason: reason}
		}
		steps = append(steps, step)
	}

	for _, t := range cfg.Targets {
		for _, k := range t.ContextKeys {
			if k == ctx.Key {
				steps = append(steps, TraceStep{Kind: TraceStepTarget, Matched: true})
				idx, val, reason := variationResult(flag, t.Variation, model.Reason{Kind: model.ReasonTargetMatch})
				return Trace{Steps: steps, Variation: idx, Value: val, Reason: reason}
			}
		}
	}
	steps = append(steps, TraceStep{Kind: TraceStepTarget, Matched: false})

	for i, rule := range cfg.Rules {
		var clauseResults []ClauseResult
		allMatch := true
		for _, clause := range rule.Clauses {
			matched := clauseMatches(&clause, ctx, store)
			clauseResults = append(clauseResults, ClauseResult{Clause: clause, Matched: matched})
			if !matched {
				allMatch = false
			}
		}
		idxCopy := i
		step := TraceStep{
			Kind:          TraceStepRule,
			Matched:       allMatch,
			RuleIndex:     &idxCopy,
			RuleID:        rule.ID,
			ClauseResults: clauseResults,
		}
		steps = append(steps, step)
		if allMatch {
			reason := model.Reason{Kind: model.ReasonRuleMatch, RuleIndex: &idxCopy, RuleID: rule.ID}
			varIdx, val, r := resolveVariationOrRollout(flag, cfg, rule.VariationOrRollout, ctx, reason)
			return Trace{Steps: steps, Variation: varIdx, Value: val, Reason: r}
		}
	}

	steps = append(steps, TraceStep{Kind: TraceStepFallthrough, Matched: true})
	idx, val, reason := resolveVariationOrRollout(flag, cfg, cfg.Fallthrough, ctx, model.Reason{Kind: model.ReasonFallthrough})
	return Trace{Steps: steps, Variation: idx, Value: val, Reason: reason}
}
