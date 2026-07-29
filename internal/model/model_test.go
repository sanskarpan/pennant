package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intPtr(i int) *int { return &i }

func TestFlag_ValidateWeightSum(t *testing.T) {
	flag := &Flag{
		Key:  "test",
		Type: TypeString,
		Variations: []Variation{
			{ID: "v0", Value: json.RawMessage(`"control"`)},
			{ID: "v1", Value: json.RawMessage(`"a"`)},
			{ID: "v2", Value: json.RawMessage(`"b"`)},
		},
		Environments: map[string]*FlagConfig{
			"prod": {
				On: true,
				Fallthrough: VariationOrRollout{
					Rollout: &Rollout{
						Variations: []WeightedVariation{
							{Variation: 0, Weight: 33334},
							{Variation: 1, Weight: 33333},
							{Variation: 2, Weight: 33333},
						},
					},
				},
			},
		},
	}
	require.NoError(t, flag.Validate())

	// Make weights not sum to 100000
	flag.Environments["prod"].Fallthrough.Rollout.Variations[0].Weight = 33333
	assert.Error(t, flag.Validate())
}

func TestFlag_ValidateVariationIndices(t *testing.T) {
	flag := &Flag{
		Key:  "test",
		Type: TypeBoolean,
		Variations: []Variation{
			{ID: "v0", Value: json.RawMessage("false")},
			{ID: "v1", Value: json.RawMessage("true")},
		},
		Environments: map[string]*FlagConfig{
			"prod": {
				On: true,
				Rules: []Rule{
					{
						ID:      "r1",
						Clauses: []Clause{{Attribute: "key", Op: OpIn, Values: []json.RawMessage{json.RawMessage(`"user-1"`)}}},
						VariationOrRollout: VariationOrRollout{Variation: intPtr(5)},
					},
				},
				Fallthrough: VariationOrRollout{Variation: intPtr(0)},
			},
		},
	}
	assert.Error(t, flag.Validate())
}

func TestContext_BuiltInAttributesShadow(t *testing.T) {
	ctx := &Context{
		Key:  "real-key",
		Kind: "user",
		Attributes: map[string]any{
			"key":  "fake-key",
			"kind": "fake-kind",
		},
	}
	v, ok := ctx.GetAttribute("key")
	assert.True(t, ok)
	assert.Equal(t, "real-key", v)

	v, ok = ctx.GetAttribute("kind")
	assert.True(t, ok)
	assert.Equal(t, "user", v)
}

func TestSegment_Validate(t *testing.T) {
	seg := &Segment{
		Key:      "beta",
		Included: []string{"user-1", "user-2"},
		Excluded: []string{"user-2"},
	}
	assert.Error(t, seg.Validate())

	seg.Excluded = []string{"user-3"}
	assert.NoError(t, seg.Validate())
}
