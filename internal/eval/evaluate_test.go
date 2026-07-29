package eval

import (
	"encoding/json"
	"fmt"
	"testing"

	"pennant/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testStore implements Store for use in tests.
type testStore struct {
	flags    map[string]*model.Flag
	segments map[string]*model.Segment
	envKey   string
}

func newTestStore(envKey string) *testStore {
	return &testStore{
		envKey:   envKey,
		flags:    make(map[string]*model.Flag),
		segments: make(map[string]*model.Segment),
	}
}

func (s *testStore) AddFlag(f *model.Flag)         { s.flags[f.Key] = f }
func (s *testStore) AddSegment(seg *model.Segment) { s.segments[seg.Key] = seg }

func (s *testStore) GetFlag(key string) (*model.Flag, *model.FlagConfig, bool) {
	f, ok := s.flags[key]
	if !ok {
		return nil, nil, false
	}
	cfg, ok := f.Environments[s.envKey]
	return f, cfg, ok
}

func (s *testStore) GetSegment(key string) (*model.Segment, bool) {
	seg, ok := s.segments[key]
	return seg, ok
}

func intPtrE(i int) *int { return &i }

// boolFlag builds a minimal boolean flag with two variations: false(0), true(1).
func boolFlag(key string, on bool, offVar *int, fallthrough_ int) *model.Flag {
	return &model.Flag{
		Key:  key,
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On:           on,
				OffVariation: offVar,
				Fallthrough:  model.VariationOrRollout{Variation: intPtrE(fallthrough_)},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// OFF state
// ---------------------------------------------------------------------------

func TestEvaluate_Off_WithOffVariation(t *testing.T) {
	store := newTestStore("prod")
	flag := boolFlag("f", false, intPtrE(0), 1)
	store.AddFlag(flag)
	cfg := flag.Environments["prod"]
	ctx := &model.Context{Kind: "user", Key: "user-1"}

	idx, val, reason := Evaluate(flag, cfg, ctx, store)
	require.NotNil(t, idx)
	assert.Equal(t, 0, *idx)
	assert.Equal(t, false, val)
	assert.Equal(t, model.ReasonOff, reason.Kind)
}

func TestEvaluate_Off_NoOffVariation(t *testing.T) {
	store := newTestStore("prod")
	flag := boolFlag("f", false, nil, 1)
	store.AddFlag(flag)
	cfg := flag.Environments["prod"]
	ctx := &model.Context{Kind: "user", Key: "user-1"}

	idx, val, reason := Evaluate(flag, cfg, ctx, store)
	assert.Nil(t, idx)
	assert.Nil(t, val)
	assert.Equal(t, model.ReasonOff, reason.Kind)
}

// ---------------------------------------------------------------------------
// Fallthrough
// ---------------------------------------------------------------------------

func TestEvaluate_On_Fallthrough(t *testing.T) {
	store := newTestStore("prod")
	flag := boolFlag("f", true, intPtrE(0), 1)
	store.AddFlag(flag)
	cfg := flag.Environments["prod"]
	ctx := &model.Context{Kind: "user", Key: "regular-user"}

	idx, val, reason := Evaluate(flag, cfg, ctx, store)
	require.NotNil(t, idx)
	assert.Equal(t, 1, *idx)
	assert.Equal(t, true, val)
	assert.Equal(t, model.ReasonFallthrough, reason.Kind)
}

// ---------------------------------------------------------------------------
// Individual targets
// ---------------------------------------------------------------------------

func TestEvaluate_Target_WinsOverRules(t *testing.T) {
	store := newTestStore("prod")
	flag := &model.Flag{
		Key:  "f",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On:      true,
				Targets: []model.Target{{Variation: 1, ContextKeys: []string{"special-user"}}},
				Rules: []model.Rule{
					{
						ID:      "r1",
						Clauses: []model.Clause{{Attribute: "key", Op: model.OpIn, Values: []json.RawMessage{json.RawMessage(`"special-user"`)}}},
						VariationOrRollout: model.VariationOrRollout{Variation: intPtrE(0)},
					},
				},
				Fallthrough: model.VariationOrRollout{Variation: intPtrE(0)},
			},
		},
	}
	store.AddFlag(flag)
	cfg := flag.Environments["prod"]

	ctx := &model.Context{Kind: "user", Key: "special-user"}
	idx, val, reason := Evaluate(flag, cfg, ctx, store)
	require.NotNil(t, idx)
	assert.Equal(t, 1, *idx)
	assert.Equal(t, true, val)
	assert.Equal(t, model.ReasonTargetMatch, reason.Kind)
}

func TestEvaluate_Target_NonMatchedUser_FallsToRule(t *testing.T) {
	store := newTestStore("prod")
	flag := &model.Flag{
		Key:  "f",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On:          true,
				Targets:     []model.Target{{Variation: 1, ContextKeys: []string{"special-user"}}},
				Fallthrough: model.VariationOrRollout{Variation: intPtrE(0)},
			},
		},
	}
	store.AddFlag(flag)
	cfg := flag.Environments["prod"]

	ctx := &model.Context{Kind: "user", Key: "ordinary-user"}
	idx, val, reason := Evaluate(flag, cfg, ctx, store)
	require.NotNil(t, idx)
	assert.Equal(t, 0, *idx)
	assert.Equal(t, false, val)
	assert.Equal(t, model.ReasonFallthrough, reason.Kind)
}

// ---------------------------------------------------------------------------
// Rules
// ---------------------------------------------------------------------------

func TestEvaluate_RuleMatch(t *testing.T) {
	store := newTestStore("prod")
	flag := &model.Flag{
		Key:  "f",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On: true,
				Rules: []model.Rule{
					{
						ID: "r1",
						Clauses: []model.Clause{
							{Attribute: "plan", Op: model.OpIn, Values: []json.RawMessage{json.RawMessage(`"enterprise"`)}},
						},
						VariationOrRollout: model.VariationOrRollout{Variation: intPtrE(1)},
					},
				},
				Fallthrough: model.VariationOrRollout{Variation: intPtrE(0)},
			},
		},
	}
	store.AddFlag(flag)
	cfg := flag.Environments["prod"]

	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{"plan": "enterprise"}}
	idx, _, reason := Evaluate(flag, cfg, ctx, store)
	require.NotNil(t, idx)
	assert.Equal(t, 1, *idx)
	assert.Equal(t, model.ReasonRuleMatch, reason.Kind)
	assert.Equal(t, "r1", reason.RuleID)

	ctx2 := &model.Context{Kind: "user", Key: "u2", Attributes: map[string]any{"plan": "free"}}
	idx2, _, reason2 := Evaluate(flag, cfg, ctx2, store)
	require.NotNil(t, idx2)
	assert.Equal(t, 0, *idx2)
	assert.Equal(t, model.ReasonFallthrough, reason2.Kind)
}

func TestEvaluate_FirstRuleWins(t *testing.T) {
	store := newTestStore("prod")
	flag := &model.Flag{
		Key:  "f",
		Type: model.TypeString,
		Variations: []model.Variation{
			{ID: "v0", Value: json.RawMessage(`"v0"`)},
			{ID: "v1", Value: json.RawMessage(`"v1"`)},
			{ID: "v2", Value: json.RawMessage(`"v2"`)},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On: true,
				Rules: []model.Rule{
					{
						ID:      "r1",
						Clauses: []model.Clause{{Attribute: "plan", Op: model.OpIn, Values: []json.RawMessage{json.RawMessage(`"enterprise"`)}}},
						VariationOrRollout: model.VariationOrRollout{Variation: intPtrE(1)},
					},
					{
						ID:      "r2",
						Clauses: []model.Clause{{Attribute: "plan", Op: model.OpIn, Values: []json.RawMessage{json.RawMessage(`"enterprise"`)}}},
						VariationOrRollout: model.VariationOrRollout{Variation: intPtrE(2)},
					},
				},
				Fallthrough: model.VariationOrRollout{Variation: intPtrE(0)},
			},
		},
	}
	store.AddFlag(flag)
	cfg := flag.Environments["prod"]

	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{"plan": "enterprise"}}
	idx, _, reason := Evaluate(flag, cfg, ctx, store)
	require.NotNil(t, idx)
	assert.Equal(t, 1, *idx, "first rule must win")
	ruleIdx := 0
	assert.Equal(t, &ruleIdx, reason.RuleIndex)
}

func TestEvaluate_EmptyRuleClause_NoMatch(t *testing.T) {
	store := newTestStore("prod")
	flag := &model.Flag{
		Key:  "f",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On: true,
				Rules: []model.Rule{
					{
						ID:                 "r1",
						Clauses:            []model.Clause{},
						VariationOrRollout: model.VariationOrRollout{Variation: intPtrE(1)},
					},
				},
				Fallthrough: model.VariationOrRollout{Variation: intPtrE(0)},
			},
		},
	}
	store.AddFlag(flag)
	cfg := flag.Environments["prod"]

	ctx := &model.Context{Kind: "user", Key: "u1"}
	idx, _, reason := Evaluate(flag, cfg, ctx, store)
	require.NotNil(t, idx)
	assert.Equal(t, 0, *idx)
	assert.Equal(t, model.ReasonFallthrough, reason.Kind)
}

// ---------------------------------------------------------------------------
// Prerequisites
// ---------------------------------------------------------------------------

func TestEvaluate_Prerequisite_Fail_OffPrereq(t *testing.T) {
	store := newTestStore("prod")
	prereq := boolFlag("prereq", false, intPtrE(0), 1)
	store.AddFlag(prereq)

	main := &model.Flag{
		Key:  "main",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On:            true,
				Prerequisites: []model.Prerequisite{{FlagKey: "prereq", Variation: 1}},
				Fallthrough:   model.VariationOrRollout{Variation: intPtrE(1)},
				OffVariation:  intPtrE(0),
			},
		},
	}
	store.AddFlag(main)
	cfg := main.Environments["prod"]
	ctx := &model.Context{Kind: "user", Key: "u1"}

	idx, _, reason := Evaluate(main, cfg, ctx, store)
	require.NotNil(t, idx)
	assert.Equal(t, 0, *idx)
	assert.Equal(t, model.ReasonPrerequisiteFailed, reason.Kind)
	assert.Equal(t, "prereq", reason.PrerequisiteKey)
}

func TestEvaluate_Prerequisite_Pass(t *testing.T) {
	store := newTestStore("prod")
	prereq := boolFlag("prereq", true, intPtrE(0), 1)
	store.AddFlag(prereq)

	main := &model.Flag{
		Key:  "main",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On:            true,
				Prerequisites: []model.Prerequisite{{FlagKey: "prereq", Variation: 1}},
				Fallthrough:   model.VariationOrRollout{Variation: intPtrE(1)},
				OffVariation:  intPtrE(0),
			},
		},
	}
	store.AddFlag(main)
	cfg := main.Environments["prod"]
	ctx := &model.Context{Kind: "user", Key: "u1"}

	idx, val, reason := Evaluate(main, cfg, ctx, store)
	require.NotNil(t, idx)
	assert.Equal(t, 1, *idx)
	assert.Equal(t, true, val)
	assert.Equal(t, model.ReasonFallthrough, reason.Kind)
}

func TestEvaluate_Prerequisite_Missing_FlagKey(t *testing.T) {
	store := newTestStore("prod")
	// prereq flag is not added to the store

	main := &model.Flag{
		Key:  "main",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On:            true,
				Prerequisites: []model.Prerequisite{{FlagKey: "nonexistent-prereq", Variation: 1}},
				Fallthrough:   model.VariationOrRollout{Variation: intPtrE(1)},
				OffVariation:  intPtrE(0),
			},
		},
	}
	store.AddFlag(main)
	cfg := main.Environments["prod"]
	ctx := &model.Context{Kind: "user", Key: "u1"}

	idx, _, reason := Evaluate(main, cfg, ctx, store)
	require.NotNil(t, idx)
	assert.Equal(t, 0, *idx)
	assert.Equal(t, model.ReasonPrerequisiteFailed, reason.Kind)
	assert.Equal(t, "nonexistent-prereq", reason.PrerequisiteKey)
}

func TestEvaluate_Prerequisite_WrongVariation(t *testing.T) {
	store := newTestStore("prod")
	// prereq is on and will fallthrough to variation 1, but we require variation 0
	prereq := boolFlag("prereq", true, intPtrE(0), 1)
	store.AddFlag(prereq)

	main := &model.Flag{
		Key:  "main",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On:            true,
				Prerequisites: []model.Prerequisite{{FlagKey: "prereq", Variation: 0}},
				Fallthrough:   model.VariationOrRollout{Variation: intPtrE(1)},
				OffVariation:  intPtrE(0),
			},
		},
	}
	store.AddFlag(main)
	cfg := main.Environments["prod"]
	ctx := &model.Context{Kind: "user", Key: "u1"}

	idx, _, reason := Evaluate(main, cfg, ctx, store)
	require.NotNil(t, idx)
	assert.Equal(t, 0, *idx)
	assert.Equal(t, model.ReasonPrerequisiteFailed, reason.Kind)
}

func TestEvaluate_PrerequisiteCycle(t *testing.T) {
	store := newTestStore("prod")
	// A flag that lists itself as a prerequisite creates a self-referential cycle.
	// The implementation detects the cycle in the recursive call, which returns the
	// off variation. The parent then sees a variation mismatch and surfaces
	// ReasonPrerequisiteFailed (with the cycle flag key) rather than propagating
	// the inner cycle error — this is the specified behaviour.
	flag := &model.Flag{
		Key:  "self-ref",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On:            true,
				Prerequisites: []model.Prerequisite{{FlagKey: "self-ref", Variation: 1}},
				Fallthrough:   model.VariationOrRollout{Variation: intPtrE(1)},
				OffVariation:  intPtrE(0),
			},
		},
	}
	store.AddFlag(flag)
	cfg := flag.Environments["prod"]
	ctx := &model.Context{Kind: "user", Key: "u1"}

	idx, _, reason := Evaluate(flag, cfg, ctx, store)
	// Cycle is detected: the recursive evaluation aborts and the outer call sees
	// a prerequisite failure (variation mismatch from the cycle-abort off-variation).
	require.NotNil(t, idx)
	assert.Equal(t, 0, *idx, "off variation returned on cycle")
	assert.Equal(t, model.ReasonPrerequisiteFailed, reason.Kind)
	assert.Equal(t, "self-ref", reason.PrerequisiteKey)
}

// ---------------------------------------------------------------------------
// Rollout
// ---------------------------------------------------------------------------

func TestEvaluate_Rollout_Distribution(t *testing.T) {
	store := newTestStore("prod")
	flag := &model.Flag{
		Key:  "rollout-flag",
		Type: model.TypeString,
		Variations: []model.Variation{
			{ID: "v0", Value: json.RawMessage(`"control"`)},
			{ID: "v1", Value: json.RawMessage(`"treatment"`)},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On:   true,
				Salt: "test-salt",
				Fallthrough: model.VariationOrRollout{
					Rollout: &model.Rollout{
						BucketBy: "key",
						Variations: []model.WeightedVariation{
							{Variation: 0, Weight: 50000},
							{Variation: 1, Weight: 50000},
						},
					},
				},
			},
		},
	}
	store.AddFlag(flag)
	cfg := flag.Environments["prod"]

	counts := [2]int{}
	for i := 0; i < 10000; i++ {
		ctx := &model.Context{Kind: "user", Key: fmt.Sprintf("user-%d", i)}
		idx, _, _ := Evaluate(flag, cfg, ctx, store)
		if idx != nil {
			counts[*idx]++
		}
	}
	total := counts[0] + counts[1]
	ratio := float64(counts[0]) / float64(total)
	assert.InDelta(t, 0.5, ratio, 0.05, "rollout should be approximately 50/50")
}

func TestEvaluate_Rollout_Seed_Overrides_FlagSalt(t *testing.T) {
	store := newTestStore("prod")
	seed := 99
	flag := &model.Flag{
		Key:  "seed-flag",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On:   true,
				Salt: "some-salt",
				Fallthrough: model.VariationOrRollout{
					Rollout: &model.Rollout{
						BucketBy: "key",
						Seed:     &seed,
						Variations: []model.WeightedVariation{
							{Variation: 0, Weight: 50000},
							{Variation: 1, Weight: 50000},
						},
					},
				},
			},
		},
	}
	store.AddFlag(flag)
	cfg := flag.Environments["prod"]

	ctx := &model.Context{Kind: "user", Key: "deterministic-user"}
	idx1, _, _ := Evaluate(flag, cfg, ctx, store)
	idx2, _, _ := Evaluate(flag, cfg, ctx, store)
	require.NotNil(t, idx1)
	require.NotNil(t, idx2)
	assert.Equal(t, *idx1, *idx2, "seed-based rollout must be deterministic")
}

func TestEvaluate_Rollout_MalformedNoVariations(t *testing.T) {
	store := newTestStore("prod")
	flag := &model.Flag{
		Key:  "bad-flag",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On: true,
				Fallthrough: model.VariationOrRollout{
					Rollout: &model.Rollout{
						BucketBy:   "key",
						Variations: []model.WeightedVariation{},
					},
				},
			},
		},
	}
	store.AddFlag(flag)
	cfg := flag.Environments["prod"]
	ctx := &model.Context{Kind: "user", Key: "u1"}

	_, _, reason := Evaluate(flag, cfg, ctx, store)
	assert.Equal(t, model.ReasonError, reason.Kind)
	assert.Equal(t, model.ErrMalformedFlag, reason.ErrorKind)
}

// ---------------------------------------------------------------------------
// clauseMatches
// ---------------------------------------------------------------------------

func TestClause_NegateMissingAttribute(t *testing.T) {
	store := newTestStore("prod")
	clause := &model.Clause{
		Attribute: "plan",
		Op:        model.OpIn,
		Values:    []json.RawMessage{json.RawMessage(`"free"`)},
		Negate:    true,
	}
	ctx := &model.Context{Kind: "user", Key: "u1"}
	result := clauseMatches(clause, ctx, store)
	assert.False(t, result, "negated clause on missing attribute must still be false")
}

func TestClause_ArrayAttr_OR(t *testing.T) {
	store := newTestStore("prod")
	clause := &model.Clause{
		Attribute: "roles",
		Op:        model.OpIn,
		Values:    []json.RawMessage{json.RawMessage(`"admin"`)},
	}
	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{
		"roles": []any{"viewer", "admin"},
	}}
	assert.True(t, clauseMatches(clause, ctx, store))
}

func TestClause_ArrayAttr_NoMatch(t *testing.T) {
	store := newTestStore("prod")
	clause := &model.Clause{
		Attribute: "roles",
		Op:        model.OpIn,
		Values:    []json.RawMessage{json.RawMessage(`"admin"`)},
	}
	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{
		"roles": []any{"viewer", "editor"},
	}}
	assert.False(t, clauseMatches(clause, ctx, store))
}

func TestClause_StringOps(t *testing.T) {
	store := newTestStore("prod")
	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{"email": "alice@acme.com"}}

	endsWith := &model.Clause{Attribute: "email", Op: model.OpEndsWith, Values: []json.RawMessage{json.RawMessage(`"@acme.com"`)}}
	assert.True(t, clauseMatches(endsWith, ctx, store))

	startsWith := &model.Clause{Attribute: "email", Op: model.OpStartsWith, Values: []json.RawMessage{json.RawMessage(`"alice"`)}}
	assert.True(t, clauseMatches(startsWith, ctx, store))

	contains := &model.Clause{Attribute: "email", Op: model.OpContains, Values: []json.RawMessage{json.RawMessage(`"acme"`)}}
	assert.True(t, clauseMatches(contains, ctx, store))
}

func TestClause_StringOps_NegatedNoMatch(t *testing.T) {
	store := newTestStore("prod")
	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{"email": "alice@other.com"}}

	endsWith := &model.Clause{
		Attribute: "email",
		Op:        model.OpEndsWith,
		Values:    []json.RawMessage{json.RawMessage(`"@acme.com"`)},
		Negate:    true,
	}
	assert.True(t, clauseMatches(endsWith, ctx, store), "negated non-match should be true")
}

func TestClause_NumericOps(t *testing.T) {
	store := newTestStore("prod")
	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{"age": float64(25)}}

	lt := &model.Clause{Attribute: "age", Op: model.OpLessThan, Values: []json.RawMessage{json.RawMessage("30")}}
	assert.True(t, clauseMatches(lt, ctx, store))

	gt := &model.Clause{Attribute: "age", Op: model.OpGreaterThan, Values: []json.RawMessage{json.RawMessage("20")}}
	assert.True(t, clauseMatches(gt, ctx, store))

	lte := &model.Clause{Attribute: "age", Op: model.OpLessThanOrEq, Values: []json.RawMessage{json.RawMessage("25")}}
	assert.True(t, clauseMatches(lte, ctx, store))

	gte := &model.Clause{Attribute: "age", Op: model.OpGreaterThanOrEq, Values: []json.RawMessage{json.RawMessage("25")}}
	assert.True(t, clauseMatches(gte, ctx, store))

	exactEq := &model.Clause{Attribute: "age", Op: model.OpLessThan, Values: []json.RawMessage{json.RawMessage("25")}}
	assert.False(t, clauseMatches(exactEq, ctx, store), "25 is not less than 25")
}

func TestClause_SemVer(t *testing.T) {
	store := newTestStore("prod")
	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{"version": "2.1.0"}}

	lt := &model.Clause{Attribute: "version", Op: model.OpSemVerLessThan, Values: []json.RawMessage{json.RawMessage(`"3.0.0"`)}}
	assert.True(t, clauseMatches(lt, ctx, store))

	eq := &model.Clause{Attribute: "version", Op: model.OpSemVerEqual, Values: []json.RawMessage{json.RawMessage(`"2.1.0"`)}}
	assert.True(t, clauseMatches(eq, ctx, store))

	gtFalse := &model.Clause{Attribute: "version", Op: model.OpSemVerGreaterThan, Values: []json.RawMessage{json.RawMessage(`"3.0.0"`)}}
	assert.False(t, clauseMatches(gtFalse, ctx, store))
}

func TestClause_Regex(t *testing.T) {
	store := newTestStore("prod")
	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{"email": "test+123@example.com"}}

	matches := &model.Clause{
		Attribute: "email",
		Op:        model.OpMatches,
		Values:    []json.RawMessage{json.RawMessage(`"^test\\+[0-9]+@example\\.com$"`)},
	}
	assert.True(t, clauseMatches(matches, ctx, store))

	noMatch := &model.Clause{
		Attribute: "email",
		Op:        model.OpMatches,
		Values:    []json.RawMessage{json.RawMessage(`"^admin@"`)},
	}
	assert.False(t, clauseMatches(noMatch, ctx, store))
}

func TestClause_Datetime_Before(t *testing.T) {
	store := newTestStore("prod")
	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{
		"created_at": "2024-01-01T00:00:00Z",
	}}

	before := &model.Clause{
		Attribute: "created_at",
		Op:        model.OpBefore,
		Values:    []json.RawMessage{json.RawMessage(`"2025-01-01T00:00:00Z"`)},
	}
	assert.True(t, clauseMatches(before, ctx, store))

	after := &model.Clause{
		Attribute: "created_at",
		Op:        model.OpAfter,
		Values:    []json.RawMessage{json.RawMessage(`"2025-01-01T00:00:00Z"`)},
	}
	assert.False(t, clauseMatches(after, ctx, store))
}

func TestClause_In_BuiltinAttributes(t *testing.T) {
	store := newTestStore("prod")
	ctx := &model.Context{Kind: "user", Key: "alice"}

	// key is a built-in attribute
	keyMatch := &model.Clause{
		Attribute: "key",
		Op:        model.OpIn,
		Values:    []json.RawMessage{json.RawMessage(`"alice"`)},
	}
	assert.True(t, clauseMatches(keyMatch, ctx, store))

	// kind is a built-in attribute
	kindMatch := &model.Clause{
		Attribute: "kind",
		Op:        model.OpIn,
		Values:    []json.RawMessage{json.RawMessage(`"user"`)},
	}
	assert.True(t, clauseMatches(kindMatch, ctx, store))
}

// ---------------------------------------------------------------------------
// segmentMatches
// ---------------------------------------------------------------------------

func TestSegment_ExcludedBeatsIncluded(t *testing.T) {
	store := newTestStore("prod")
	seg := &model.Segment{
		Key:      "beta",
		Included: []string{"user-1"},
		Excluded: []string{"user-1"},
	}
	store.AddSegment(seg)
	ctx := &model.Context{Kind: "user", Key: "user-1"}
	result := segmentMatches(seg, ctx, store)
	assert.False(t, result, "excluded must beat included")
}

func TestSegment_Included(t *testing.T) {
	store := newTestStore("prod")
	seg := &model.Segment{
		Key:      "beta",
		Included: []string{"user-1", "user-2"},
	}
	store.AddSegment(seg)

	ctx1 := &model.Context{Kind: "user", Key: "user-1"}
	assert.True(t, segmentMatches(seg, ctx1, store))

	ctx3 := &model.Context{Kind: "user", Key: "user-3"}
	assert.False(t, segmentMatches(seg, ctx3, store))
}

func TestSegment_Excluded(t *testing.T) {
	store := newTestStore("prod")
	seg := &model.Segment{
		Key:      "beta",
		Excluded: []string{"banned-user"},
	}
	store.AddSegment(seg)

	ctx := &model.Context{Kind: "user", Key: "banned-user"}
	assert.False(t, segmentMatches(seg, ctx, store))
}

func TestSegment_Rule_ClauseMatch(t *testing.T) {
	store := newTestStore("prod")
	seg := &model.Segment{
		Key: "enterprise",
		Rules: []model.SegmentRule{
			{
				ID: "sr1",
				Clauses: []model.Clause{
					{Attribute: "plan", Op: model.OpIn, Values: []json.RawMessage{json.RawMessage(`"enterprise"`)}},
				},
			},
		},
	}
	store.AddSegment(seg)

	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{"plan": "enterprise"}}
	assert.True(t, segmentMatches(seg, ctx, store))

	ctx2 := &model.Context{Kind: "user", Key: "u2", Attributes: map[string]any{"plan": "free"}}
	assert.False(t, segmentMatches(seg, ctx2, store))
}

func TestSegment_Rule_WithWeight(t *testing.T) {
	store := newTestStore("prod")
	weight := 50000 // 50%
	seg := &model.Segment{
		Key:  "half-rollout",
		Salt: "seg-salt",
		Rules: []model.SegmentRule{
			{
				ID:       "sr1",
				BucketBy: "key",
				Weight:   &weight,
				Clauses: []model.Clause{
					{Attribute: "plan", Op: model.OpIn, Values: []json.RawMessage{json.RawMessage(`"pro"`)}},
				},
			},
		},
	}
	store.AddSegment(seg)

	matched, total := 0, 0
	for i := 0; i < 5000; i++ {
		ctx := &model.Context{Kind: "user", Key: fmt.Sprintf("pro-user-%d", i), Attributes: map[string]any{"plan": "pro"}}
		if segmentMatches(seg, ctx, store) {
			matched++
		}
		total++
	}
	ratio := float64(matched) / float64(total)
	assert.InDelta(t, 0.5, ratio, 0.05, "weighted segment rule should be approximately 50%%")
}

func TestClause_SegmentMatch(t *testing.T) {
	store := newTestStore("prod")
	seg := &model.Segment{
		Key:      "vip",
		Included: []string{"vip-user"},
	}
	store.AddSegment(seg)

	clause := &model.Clause{
		Attribute: "",
		Op:        model.OpSegmentMatch,
		Values:    []json.RawMessage{json.RawMessage(`"vip"`)},
	}

	ctx := &model.Context{Kind: "user", Key: "vip-user"}
	assert.True(t, clauseMatches(clause, ctx, store))

	ctx2 := &model.Context{Kind: "user", Key: "regular-user"}
	assert.False(t, clauseMatches(clause, ctx2, store))
}

func TestClause_SegmentMatch_Negated(t *testing.T) {
	store := newTestStore("prod")
	seg := &model.Segment{
		Key:      "blocked",
		Included: []string{"blocked-user"},
	}
	store.AddSegment(seg)

	clause := &model.Clause{
		Op:     model.OpSegmentMatch,
		Values: []json.RawMessage{json.RawMessage(`"blocked"`)},
		Negate: true,
	}

	// regular user not in segment: NOT in blocked = true
	ctx := &model.Context{Kind: "user", Key: "normal-user"}
	assert.True(t, clauseMatches(clause, ctx, store))

	// blocked user IS in segment: NOT in blocked = false
	ctx2 := &model.Context{Kind: "user", Key: "blocked-user"}
	assert.False(t, clauseMatches(clause, ctx2, store))
}

// ---------------------------------------------------------------------------
// ruleMatches
// ---------------------------------------------------------------------------

func TestRuleMatches_AllClausesRequired(t *testing.T) {
	store := newTestStore("prod")
	rule := &model.Rule{
		ID: "r1",
		Clauses: []model.Clause{
			{Attribute: "plan", Op: model.OpIn, Values: []json.RawMessage{json.RawMessage(`"enterprise"`)}},
			{Attribute: "country", Op: model.OpIn, Values: []json.RawMessage{json.RawMessage(`"US"`)}},
		},
	}

	// Both match
	ctx1 := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{"plan": "enterprise", "country": "US"}}
	assert.True(t, ruleMatches(rule, ctx1, store))

	// Only first matches
	ctx2 := &model.Context{Kind: "user", Key: "u2", Attributes: map[string]any{"plan": "enterprise", "country": "CA"}}
	assert.False(t, ruleMatches(rule, ctx2, store))

	// Neither matches
	ctx3 := &model.Context{Kind: "user", Key: "u3", Attributes: map[string]any{"plan": "free", "country": "CA"}}
	assert.False(t, ruleMatches(rule, ctx3, store))
}

func TestRuleMatches_EmptyClauses_ReturnsFalse(t *testing.T) {
	store := newTestStore("prod")
	rule := &model.Rule{ID: "r1", Clauses: []model.Clause{}}
	ctx := &model.Context{Kind: "user", Key: "u1"}
	assert.False(t, ruleMatches(rule, ctx, store))
}
