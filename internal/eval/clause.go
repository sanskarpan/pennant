package eval

import (
	"encoding/json"

	"pennant/internal/model"
)

// Store is the interface eval needs to look up flags and segments.
type Store interface {
	GetFlag(key string) (*model.Flag, *model.FlagConfig, bool)
	GetSegment(key string) (*model.Segment, bool)
}

func clauseMatches(c *model.Clause, ctx *model.Context, store Store) bool {
	if c.Op == model.OpSegmentMatch {
		for _, raw := range c.Values {
			var segKey string
			if json.Unmarshal(raw, &segKey) != nil {
				continue
			}
			if seg, ok := store.GetSegment(segKey); ok && segmentMatches(seg, ctx, store) {
				return !c.Negate
			}
		}
		return c.Negate
	}

	attrValue, found := ctx.GetAttribute(c.Attribute)
	if !found {
		// CRITICAL: missing attribute is ALWAYS a non-match, even when negated.
		// NOT (plan in [free]) must NOT match a context with no plan attribute.
		return false
	}

	if arr, ok := attrValue.([]any); ok {
		for _, elem := range arr {
			for _, v := range c.Values {
				if matchOperator(c.Op, elem, v) {
					return !c.Negate
				}
			}
		}
		return c.Negate
	}

	for _, v := range c.Values {
		if matchOperator(c.Op, attrValue, v) {
			return !c.Negate
		}
	}
	return c.Negate
}

func ruleMatches(r *model.Rule, ctx *model.Context, store Store) bool {
	if len(r.Clauses) == 0 {
		return false
	}
	for i := range r.Clauses {
		if !clauseMatches(&r.Clauses[i], ctx, store) {
			return false
		}
	}
	return true
}

// segmentMatches: EXCLUDED ALWAYS WINS over INCLUDED.
func segmentMatches(seg *model.Segment, ctx *model.Context, store Store) bool {
	for _, k := range seg.Excluded {
		if k == ctx.Key {
			return false
		}
	}
	for _, k := range seg.Included {
		if k == ctx.Key {
			return true
		}
	}
	for _, rule := range seg.Rules {
		allMatch := true
		for i := range rule.Clauses {
			if !clauseMatches(&rule.Clauses[i], ctx, store) {
				allMatch = false
				break
			}
		}
		if !allMatch {
			continue
		}
		if rule.Weight == nil {
			return true
		}
		bucketBy := rule.BucketBy
		if bucketBy == "" {
			bucketBy = "key"
		}
		b := ComputeBucket(ctx, bucketBy, seg.Key, seg.Salt, nil)
		return b < float64(*rule.Weight)/100000.0
	}
	return false
}
