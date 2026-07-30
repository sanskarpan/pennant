package eval

import (
	"encoding/json"
	"testing"

	"pennant/internal/model"
)

// FuzzEvaluateFlag tests the eval engine against random flag configs.
// Run with: go test ./internal/eval -fuzz=FuzzEvaluateFlag -fuzztime=30s
func FuzzEvaluateFlag(f *testing.F) {
	// Seed 1: minimal boolean flag, on, fallthrough variation 1
	offVar0 := 0
	trueVar1 := 1
	seed1, _ := json.Marshal(&model.Flag{
		Key:  "seed-flag",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On:           true,
				Fallthrough:  model.VariationOrRollout{Variation: &trueVar1},
				OffVariation: &offVar0,
			},
		},
	})
	f.Add(seed1, "user-abc", "enterprise")

	// Seed 2: flag that is off
	offVar02 := 0
	trueVar12 := 1
	seed2, _ := json.Marshal(&model.Flag{
		Key:  "off-flag",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "false", Value: json.RawMessage("false")},
			{ID: "true", Value: json.RawMessage("true")},
		},
		Environments: map[string]*model.FlagConfig{
			"prod": {
				On:           false,
				OffVariation: &offVar02,
				Fallthrough:  model.VariationOrRollout{Variation: &trueVar12},
			},
		},
	})
	f.Add(seed2, "user-xyz", "free")

	// Seed 3: flag with a rule
	v0idx := 0
	v1idx := 1
	seed3, _ := json.Marshal(&model.Flag{
		Key:  "rule-flag",
		Type: model.TypeString,
		Variations: []model.Variation{
			{ID: "v0", Value: json.RawMessage(`"control"`)},
			{ID: "v1", Value: json.RawMessage(`"treatment"`)},
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
						VariationOrRollout: model.VariationOrRollout{Variation: &v1idx},
					},
				},
				Fallthrough: model.VariationOrRollout{Variation: &v0idx},
			},
		},
	})
	f.Add(seed3, "user-1", "enterprise")

	f.Fuzz(func(t *testing.T, flagJSON []byte, userKey string, planValue string) {
		// If the JSON is invalid, skip — not a bug in the eval engine.
		var flag model.Flag
		if err := json.Unmarshal(flagJSON, &flag); err != nil {
			return
		}
		// Must have at least 2 variations to be meaningful.
		if len(flag.Variations) < 2 {
			return
		}
		cfg, ok := flag.Environments["prod"]
		if !ok {
			return
		}

		ctx := &model.Context{
			Kind: "user",
			Key:  userKey,
			Attributes: map[string]any{
				"plan": planValue,
			},
		}

		store := newTestStore("prod")
		store.AddFlag(&flag)

		// Must never panic.
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic during evaluation of flag %q: %v", flag.Key, r)
			}
		}()

		// Call the evaluator — result is ignored; we only care about no panics.
		_, _, _ = Evaluate(&flag, cfg, ctx, store)
	})
}
