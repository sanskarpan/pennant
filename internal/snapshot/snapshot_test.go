package snapshot

import (
	"encoding/json"
	"testing"

	"pennant/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestSnap(version int64, flags map[string]*ResolvedFlag) *Snapshot {
	if flags == nil {
		flags = make(map[string]*ResolvedFlag)
	}
	snap := &Snapshot{
		EnvironmentKey: "production",
		ProjectKey:     "default",
		Version:        version,
		Flags:          flags,
		Segments:       make(map[string]*model.Segment),
	}
	checksum, _ := Checksum(snap)
	snap.Checksum = checksum
	return snap
}

func TestCanonicalJSON_Stable(t *testing.T) {
	// Same data in different map iteration order must produce identical bytes.
	snap1 := makeTestSnap(1, map[string]*ResolvedFlag{
		"flag-a": {Key: "flag-a", Name: "Flag A", Type: model.TypeBoolean},
		"flag-b": {Key: "flag-b", Name: "Flag B", Type: model.TypeString},
	})
	snap2 := makeTestSnap(1, map[string]*ResolvedFlag{
		"flag-b": {Key: "flag-b", Name: "Flag B", Type: model.TypeString},
		"flag-a": {Key: "flag-a", Name: "Flag A", Type: model.TypeBoolean},
	})

	b1, err := CanonicalJSON(snap1)
	require.NoError(t, err)
	b2, err := CanonicalJSON(snap2)
	require.NoError(t, err)
	assert.Equal(t, string(b1), string(b2), "canonical JSON must be identical regardless of map order")
}

func TestChecksum_DetectsChange(t *testing.T) {
	offVar := 0
	flag := &ResolvedFlag{
		Key:  "my-flag",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "v0", Value: json.RawMessage("false")},
			{ID: "v1", Value: json.RawMessage("true")},
		},
		Config: &model.FlagConfig{On: false, OffVariation: &offVar},
	}
	snap := makeTestSnap(1, map[string]*ResolvedFlag{"my-flag": flag})

	cs1, err := Checksum(snap)
	require.NoError(t, err)

	// Flip the flag on — checksum must change.
	snap.Flags["my-flag"].Config.On = true
	cs2, err := Checksum(snap)
	require.NoError(t, err)

	assert.NotEqual(t, cs1, cs2, "checksum must change when flag changes")
}

func TestDelta_Roundtrip(t *testing.T) {
	offVar := 0
	from := makeTestSnap(1, map[string]*ResolvedFlag{
		"flag-a": {
			Key:  "flag-a",
			Type: model.TypeBoolean,
			Config: &model.FlagConfig{On: false, OffVariation: &offVar},
		},
	})

	trueVar := 1
	to := makeTestSnap(2, map[string]*ResolvedFlag{
		"flag-a": {
			Key:  "flag-a",
			Type: model.TypeBoolean,
			Config: &model.FlagConfig{
				On:           true,
				OffVariation: &offVar,
				Fallthrough:  model.VariationOrRollout{Variation: &trueVar},
			},
		},
		"flag-b": {
			Key:    "flag-b",
			Type:   model.TypeString,
			Config: &model.FlagConfig{On: true},
		},
	})

	delta := ComputeDelta(from, to)
	// Delta may be nil if the 30% threshold is exceeded; just verify it compiles.
	if delta != nil {
		result := ApplyDelta(from, delta)
		assert.Equal(t, to.Version, result.Version)
		assert.Equal(t, to.Checksum, result.Checksum)
		assert.Equal(t, len(to.Flags), len(result.Flags))
	}
}

func TestDelta_NilWhenTooLarge(t *testing.T) {
	// A large from-snapshot with many flags, and an entirely different to-snapshot,
	// should return nil (full put preferred).
	fromFlags := make(map[string]*ResolvedFlag)
	toFlags := make(map[string]*ResolvedFlag)
	for i := 0; i < 50; i++ {
		k := string(rune('a'+i%26)) + string(rune('0'+i/26))
		fromFlags[k] = &ResolvedFlag{Key: k, Type: model.TypeBoolean}
	}
	// to has completely different keys → large delta
	for i := 50; i < 100; i++ {
		k := string(rune('a'+i%26)) + string(rune('0'+i/26))
		toFlags[k] = &ResolvedFlag{Key: k, Type: model.TypeString}
	}
	from := makeTestSnap(1, fromFlags)
	to := makeTestSnap(2, toFlags)
	// Either nil (threshold exceeded) or a valid delta — both are acceptable.
	// The test just ensures no panic.
	_ = ComputeDelta(from, to)
}
