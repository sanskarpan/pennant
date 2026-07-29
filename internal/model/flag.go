package model

import (
	"encoding/json"
	"fmt"
	"time"
)

type VariationType string

const (
	TypeBoolean VariationType = "boolean"
	TypeString  VariationType = "string"
	TypeNumber  VariationType = "number"
	TypeJSON    VariationType = "json"
)

type Variation struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Value       json.RawMessage `json:"value"`
}

type Flag struct {
	Key          string                 `json:"key"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Type         VariationType          `json:"type"`
	Variations   []Variation            `json:"variations"`
	Tags         []string               `json:"tags"`
	Temporary    bool                   `json:"temporary"`
	Archived     bool                   `json:"archived"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Environments map[string]*FlagConfig `json:"environments"`
}

type FlagConfig struct {
	On            bool               `json:"on"`
	Prerequisites []Prerequisite     `json:"prerequisites"`
	Targets       []Target           `json:"targets"`
	Rules         []Rule             `json:"rules"`
	Fallthrough   VariationOrRollout `json:"fallthrough"`
	OffVariation  *int               `json:"off_variation"`
	Salt          string             `json:"salt"`
	TrackEvents   bool               `json:"track_events"`
	Version       int64              `json:"version"`
}

type Prerequisite struct {
	FlagKey   string `json:"flag_key"`
	Variation int    `json:"variation"`
}

type Target struct {
	Variation   int      `json:"variation"`
	ContextKeys []string `json:"context_keys"`
}

type Rule struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Clauses     []Clause `json:"clauses"`
	VariationOrRollout
}

type VariationOrRollout struct {
	Variation *int     `json:"variation,omitempty"`
	Rollout   *Rollout `json:"rollout,omitempty"`
}

type Rollout struct {
	Variations   []WeightedVariation `json:"variations"`
	BucketBy     string              `json:"bucket_by"`
	Seed         *int                `json:"seed"`
	IsExperiment bool                `json:"is_experiment,omitempty"`
}

type WeightedVariation struct {
	Variation int `json:"variation"`
	Weight    int `json:"weight"`
}

func (f *Flag) Validate() error {
	if len(f.Variations) < 2 {
		return fmt.Errorf("flag %q must have at least 2 variations, got %d", f.Key, len(f.Variations))
	}
	for envKey, env := range f.Environments {
		if err := f.validateFlagConfig(envKey, env); err != nil {
			return err
		}
	}
	return nil
}

func (f *Flag) validateFlagConfig(envKey string, cfg *FlagConfig) error {
	if cfg.OffVariation != nil {
		if *cfg.OffVariation < 0 || *cfg.OffVariation >= len(f.Variations) {
			return fmt.Errorf("env %q: off_variation index %d out of range", envKey, *cfg.OffVariation)
		}
	}
	ruleIDs := make(map[string]bool)
	for i, rule := range cfg.Rules {
		if rule.ID == "" {
			return fmt.Errorf("env %q: rule at index %d has empty ID", envKey, i)
		}
		if ruleIDs[rule.ID] {
			return fmt.Errorf("env %q: duplicate rule ID %q", envKey, rule.ID)
		}
		ruleIDs[rule.ID] = true
		if rule.Variation != nil {
			if *rule.Variation < 0 || *rule.Variation >= len(f.Variations) {
				return fmt.Errorf("env %q rule %q: variation index %d out of range", envKey, rule.ID, *rule.Variation)
			}
		}
		if rule.Rollout != nil {
			if err := f.validateRollout(envKey, rule.ID, rule.Rollout); err != nil {
				return err
			}
		}
	}
	if cfg.Fallthrough.Rollout != nil {
		if err := f.validateRollout(envKey, "fallthrough", cfg.Fallthrough.Rollout); err != nil {
			return err
		}
	}
	return nil
}

func (f *Flag) validateRollout(envKey, ruleID string, ro *Rollout) error {
	sum := 0
	for _, wv := range ro.Variations {
		if wv.Variation < 0 || wv.Variation >= len(f.Variations) {
			return fmt.Errorf("env %q rule %q: rollout variation index %d out of range", envKey, ruleID, wv.Variation)
		}
		sum += wv.Weight
	}
	if sum != 100000 {
		return fmt.Errorf("env %q rule %q: rollout weights sum to %d, must be 100000", envKey, ruleID, sum)
	}
	return nil
}
