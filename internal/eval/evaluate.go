package eval

import (
	"encoding/json"

	"pennant/internal/model"
)

const MaxPrerequisiteDepth = 20

func Evaluate(flag *model.Flag, cfg *model.FlagConfig, ctx *model.Context, store Store) (*int, any, model.Reason) {
	return evaluateWithDepth(flag, cfg, ctx, store, 0, map[string]bool{})
}

func evaluateWithDepth(
	flag *model.Flag, cfg *model.FlagConfig, ctx *model.Context, store Store,
	depth int, visited map[string]bool,
) (*int, any, model.Reason) {

	// STEP 1: OFF
	if !cfg.On {
		return offResult(flag, cfg, model.Reason{Kind: model.ReasonOff})
	}

	// STEP 2: PREREQUISITES
	if depth > MaxPrerequisiteDepth || visited[flag.Key] {
		return offResult(flag, cfg, model.Reason{Kind: model.ReasonError, ErrorKind: model.ErrPrereqCycle})
	}
	visited[flag.Key] = true

	for _, p := range cfg.Prerequisites {
		pf, pcfg, ok := store.GetFlag(p.FlagKey)
		if !ok {
			return offResult(flag, cfg, model.Reason{Kind: model.ReasonPrerequisiteFailed, PrerequisiteKey: p.FlagKey})
		}
		if !pcfg.On {
			return offResult(flag, cfg, model.Reason{Kind: model.ReasonPrerequisiteFailed, PrerequisiteKey: p.FlagKey})
		}
		// Pass a copy so sibling prereqs don't see each other's visited sets.
		pIdx, _, _ := evaluateWithDepth(pf, pcfg, ctx, store, depth+1, copyVisited(visited))
		if pIdx == nil || *pIdx != p.Variation {
			return offResult(flag, cfg, model.Reason{Kind: model.ReasonPrerequisiteFailed, PrerequisiteKey: p.FlagKey})
		}
	}

	// STEP 3: INDIVIDUAL TARGETS (checked BEFORE rules, always win)
	for _, t := range cfg.Targets {
		for _, k := range t.ContextKeys {
			if k == ctx.Key {
				return variationResult(flag, t.Variation, model.Reason{Kind: model.ReasonTargetMatch})
			}
		}
	}

	// STEP 4: RULES — FIRST MATCH WINS, IN STORED ORDER
	for i, rule := range cfg.Rules {
		if ruleMatches(&rule, ctx, store) {
			idx := i
			reason := model.Reason{Kind: model.ReasonRuleMatch, RuleIndex: &idx, RuleID: rule.ID}
			return resolveVariationOrRollout(flag, cfg, rule.VariationOrRollout, ctx, reason)
		}
	}

	// STEP 5: FALLTHROUGH
	return resolveVariationOrRollout(flag, cfg, cfg.Fallthrough, ctx, model.Reason{Kind: model.ReasonFallthrough})
}

func resolveVariationOrRollout(
	flag *model.Flag, cfg *model.FlagConfig, vr model.VariationOrRollout, ctx *model.Context, reason model.Reason,
) (*int, any, model.Reason) {
	if vr.Variation != nil {
		return variationResult(flag, *vr.Variation, reason)
	}
	if vr.Rollout == nil || len(vr.Rollout.Variations) == 0 {
		return nil, nil, model.Reason{Kind: model.ReasonError, ErrorKind: model.ErrMalformedFlag}
	}

	ro := vr.Rollout
	bucketBy := ro.BucketBy
	if bucketBy == "" {
		bucketBy = "key"
	}
	bucket := ComputeBucket(ctx, bucketBy, flag.Key, cfg.Salt, ro.Seed)
	reason.InExperiment = ro.IsExperiment

	sum := 0.0
	for _, wv := range ro.Variations {
		sum += float64(wv.Weight) / 100000.0
		if bucket < sum {
			return variationResult(flag, wv.Variation, reason)
		}
	}

	// FLOATING-POINT SAFETY NET: return last variation if bucket rounds to 1.0
	last := ro.Variations[len(ro.Variations)-1]
	return variationResult(flag, last.Variation, reason)
}

func copyVisited(v map[string]bool) map[string]bool {
	cp := make(map[string]bool, len(v))
	for k := range v {
		cp[k] = true
	}
	return cp
}

func offResult(flag *model.Flag, cfg *model.FlagConfig, reason model.Reason) (*int, any, model.Reason) {
	if cfg.OffVariation == nil {
		return nil, nil, reason
	}
	return variationResult(flag, *cfg.OffVariation, reason)
}

func variationResult(flag *model.Flag, idx int, reason model.Reason) (*int, any, model.Reason) {
	if idx < 0 || idx >= len(flag.Variations) {
		return nil, nil, model.Reason{Kind: model.ReasonError, ErrorKind: model.ErrMalformedFlag}
	}
	var val any
	_ = json.Unmarshal(flag.Variations[idx].Value, &val)
	return &idx, val, reason
}
