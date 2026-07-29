package pennant_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"pennant/internal/eval"
	"pennant/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// conformanceFixture matches the conformance JSON format.
// Note: field names must match actual model JSON tags, which may differ from
// the fixture's top-level field names (flag, flagConfig etc. are loader-specific).
type conformanceFixture struct {
	Description   string                     `json:"description"`
	Flag          *conformanceFlag           `json:"flag"`
	FlagConfig    *conformanceFlagConfig     `json:"flagConfig"`
	Context       *model.Context             `json:"context"`
	Segments      map[string]*model.Segment  `json:"segments"`
	Prerequisites map[string]*prereqEntry    `json:"prerequisites"`
	Expected      conformanceExpected        `json:"expected"`
}

// conformanceFlag is a thin wrapper that lets us unmarshal fixture JSON
// (variations with plain json values) into model.Flag.
type conformanceFlag struct {
	Key          string              `json:"key"`
	Name         string              `json:"name"`
	Type         model.VariationType `json:"type"`
	Variations   []conformanceVar    `json:"variations"`
	Environments map[string]any      `json:"environments"`
}

type conformanceVar struct {
	ID    string          `json:"id"`
	Value json.RawMessage `json:"value"`
}

// conformanceFlagConfig matches the fixture JSON field names.
// The real model.FlagConfig uses snake_case JSON tags like off_variation, flag_key, etc.
type conformanceFlagConfig struct {
	On            bool                     `json:"on"`
	OffVariation  *int                     `json:"off_variation"`
	Prerequisites []conformancePrereq      `json:"prerequisites"`
	Targets       []conformanceTarget      `json:"targets"`
	Rules         []conformanceRule        `json:"rules"`
	Fallthrough   conformanceVorR          `json:"fallthrough"`
	Salt          string                   `json:"salt"`
}

type conformancePrereq struct {
	FlagKey   string `json:"flag_key"`
	Variation int    `json:"variation"`
}

type conformanceTarget struct {
	ContextKeys []string `json:"context_keys"`
	Variation   int      `json:"variation"`
}

type conformanceRule struct {
	ID      string               `json:"id"`
	Clauses []conformanceClause  `json:"clauses"`
	// VariationOrRollout inlined
	Variation *int               `json:"variation,omitempty"`
	Rollout   *conformanceRollout `json:"rollout,omitempty"`
}

type conformanceClause struct {
	Attribute string            `json:"attribute"`
	Op        model.Operator    `json:"op"`
	Values    []json.RawMessage `json:"values"`
	Negate    bool              `json:"negate"`
}

type conformanceVorR struct {
	Variation *int                `json:"variation,omitempty"`
	Rollout   *conformanceRollout `json:"rollout,omitempty"`
}

type conformanceRollout struct {
	Variations []model.WeightedVariation `json:"variations"`
	BucketBy   string                    `json:"bucket_by"`
	Seed       *int                      `json:"seed"`
}

type prereqEntry struct {
	Flag       *conformanceFlag       `json:"flag"`
	FlagConfig *conformanceFlagConfig `json:"flagConfig"`
}

type conformanceExpected struct {
	VariationIndex *int    `json:"variationIndex"`
	Value          any     `json:"value"`
	Reason         string  `json:"reason"`
	RuleIndex      *int    `json:"ruleIndex"`
	ErrorKind      *string `json:"errorKind"`
}

// conformanceStore implements eval.Store for conformance tests.
type conformanceStore struct {
	flags    map[string]*model.Flag
	configs  map[string]*model.FlagConfig
	segments map[string]*model.Segment
}

func (s *conformanceStore) GetFlag(key string) (*model.Flag, *model.FlagConfig, bool) {
	f, ok := s.flags[key]
	if !ok {
		return nil, nil, false
	}
	cfg, ok := s.configs[key]
	if !ok {
		return nil, nil, false
	}
	return f, cfg, true
}

func (s *conformanceStore) GetSegment(key string) (*model.Segment, bool) {
	seg, ok := s.segments[key]
	return seg, ok
}

// toModelFlag converts the fixture flag type into model.Flag.
func toModelFlag(cf *conformanceFlag) *model.Flag {
	if cf == nil {
		return nil
	}
	vars := make([]model.Variation, len(cf.Variations))
	for i, v := range cf.Variations {
		vars[i] = model.Variation{ID: v.ID, Value: v.Value}
	}
	return &model.Flag{
		Key:          cf.Key,
		Name:         cf.Name,
		Type:         cf.Type,
		Variations:   vars,
		Environments: map[string]*model.FlagConfig{},
	}
}

// toModelFlagConfig converts the fixture config type into model.FlagConfig.
func toModelFlagConfig(cc *conformanceFlagConfig) *model.FlagConfig {
	if cc == nil {
		return nil
	}
	prereqs := make([]model.Prerequisite, len(cc.Prerequisites))
	for i, p := range cc.Prerequisites {
		prereqs[i] = model.Prerequisite{FlagKey: p.FlagKey, Variation: p.Variation}
	}
	targets := make([]model.Target, len(cc.Targets))
	for i, t := range cc.Targets {
		targets[i] = model.Target{ContextKeys: t.ContextKeys, Variation: t.Variation}
	}
	rules := make([]model.Rule, len(cc.Rules))
	for i, r := range cc.Rules {
		clauses := make([]model.Clause, len(r.Clauses))
		for j, cl := range r.Clauses {
			clauses[j] = model.Clause{
				Attribute: cl.Attribute,
				Op:        cl.Op,
				Values:    cl.Values,
				Negate:    cl.Negate,
			}
		}
		rule := model.Rule{
			ID:      r.ID,
			Clauses: clauses,
		}
		if r.Variation != nil {
			rule.VariationOrRollout = model.VariationOrRollout{Variation: r.Variation}
		} else if r.Rollout != nil {
			rule.VariationOrRollout = model.VariationOrRollout{
				Rollout: &model.Rollout{
					Variations: r.Rollout.Variations,
					BucketBy:   r.Rollout.BucketBy,
					Seed:       r.Rollout.Seed,
				},
			}
		}
		rules[i] = rule
	}

	var ft model.VariationOrRollout
	if cc.Fallthrough.Variation != nil {
		ft = model.VariationOrRollout{Variation: cc.Fallthrough.Variation}
	} else if cc.Fallthrough.Rollout != nil {
		ft = model.VariationOrRollout{
			Rollout: &model.Rollout{
				Variations: cc.Fallthrough.Rollout.Variations,
				BucketBy:   cc.Fallthrough.Rollout.BucketBy,
				Seed:       cc.Fallthrough.Rollout.Seed,
			},
		}
	}

	return &model.FlagConfig{
		On:            cc.On,
		OffVariation:  cc.OffVariation,
		Prerequisites: prereqs,
		Targets:       targets,
		Rules:         rules,
		Fallthrough:   ft,
		Salt:          cc.Salt,
	}
}

func TestConformance(t *testing.T) {
	conformanceDir := filepath.Join("conformance")

	type fixtureEntry struct {
		path    string
		fixture conformanceFixture
	}
	var fixtures []fixtureEntry

	err := filepath.Walk(conformanceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var fix conformanceFixture
		if err := json.Unmarshal(data, &fix); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		fixtures = append(fixtures, fixtureEntry{path, fix})
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, len(fixtures), 0, "no conformance fixtures found")

	for _, tc := range fixtures {
		tc := tc // capture
		rel, _ := filepath.Rel(conformanceDir, tc.path)
		testName := rel + " / " + tc.fixture.Description
		t.Run(testName, func(t *testing.T) {
			// Build store
			store := &conformanceStore{
				flags:    make(map[string]*model.Flag),
				configs:  make(map[string]*model.FlagConfig),
				segments: make(map[string]*model.Segment),
			}

			// Convert and register main flag
			mainFlag := toModelFlag(tc.fixture.Flag)
			mainConfig := toModelFlagConfig(tc.fixture.FlagConfig)
			require.NotNil(t, mainFlag, "fixture must have a flag")
			require.NotNil(t, mainConfig, "fixture must have a flagConfig")

			store.flags[mainFlag.Key] = mainFlag
			store.configs[mainFlag.Key] = mainConfig

			// Register prerequisite flags
			for key, entry := range tc.fixture.Prerequisites {
				pf := toModelFlag(entry.Flag)
				pc := toModelFlagConfig(entry.FlagConfig)
				if pf != nil {
					store.flags[key] = pf
				}
				if pc != nil {
					store.configs[key] = pc
				}
			}

			// Register segments
			for key, seg := range tc.fixture.Segments {
				store.segments[key] = seg
			}

			varIdx, value, reason := eval.Evaluate(mainFlag, mainConfig, tc.fixture.Context, store)

			// Check variation index
			if tc.fixture.Expected.VariationIndex == nil {
				assert.Nil(t, varIdx, "expected nil variation index")
			} else {
				require.NotNil(t, varIdx, "expected non-nil variation index")
				assert.Equal(t, *tc.fixture.Expected.VariationIndex, *varIdx, "variation index mismatch")
			}

			// Check reason kind
			assert.Equal(t, model.ReasonKind(tc.fixture.Expected.Reason), reason.Kind, "reason kind mismatch")

			// Check rule index if specified
			if tc.fixture.Expected.RuleIndex != nil {
				require.NotNil(t, reason.RuleIndex, "expected non-nil ruleIndex in reason")
				assert.Equal(t, *tc.fixture.Expected.RuleIndex, *reason.RuleIndex, "ruleIndex mismatch")
			}

			// Check error kind if specified
			if tc.fixture.Expected.ErrorKind != nil {
				assert.Equal(t, model.ErrorKind(*tc.fixture.Expected.ErrorKind), reason.ErrorKind, "error kind mismatch")
			}

			// Check value (marshal both to JSON for stable comparison)
			if tc.fixture.Expected.VariationIndex != nil {
				expectedBytes, _ := json.Marshal(tc.fixture.Expected.Value)
				actualBytes, _ := json.Marshal(value)
				assert.Equal(t, string(expectedBytes), string(actualBytes), "value mismatch")
			}
		})
	}
}
