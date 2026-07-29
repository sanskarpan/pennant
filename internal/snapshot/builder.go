package snapshot

import (
	"pennant/internal/model"
	"pennant/internal/store"
	"time"
)

// Builder builds Snapshots from the config store.
type Builder struct {
	store store.ConfigStore
}

// NewBuilder returns a Builder backed by the given ConfigStore.
func NewBuilder(s store.ConfigStore) *Builder {
	return &Builder{store: s}
}

// Build constructs a complete Snapshot for a project+environment pair.
// Archived flags are excluded. Flags without an explicit FlagConfig for the
// environment receive a default "off" config.
func (b *Builder) Build(projectKey, envKey string) (*Snapshot, error) {
	version, err := b.store.GetEnvVersion(projectKey, envKey)
	if err != nil {
		return nil, err
	}

	flags, err := b.store.ListFlags(projectKey)
	if err != nil {
		return nil, err
	}

	segments, err := b.store.ListSegments(projectKey)
	if err != nil {
		return nil, err
	}

	resolvedFlags := make(map[string]*ResolvedFlag, len(flags))
	for _, f := range flags {
		if f.Archived {
			continue
		}
		cfg, err := b.store.GetFlagConfig(projectKey, envKey, f.Key)
		if err != nil {
			// No config for this env yet — serve a default "off" config.
			offVar := 0
			cfg = &model.FlagConfig{
				On:           false,
				OffVariation: &offVar,
			}
		}
		resolvedFlags[f.Key] = &ResolvedFlag{
			Key:         f.Key,
			Name:        f.Name,
			Description: f.Description,
			Type:        f.Type,
			Variations:  f.Variations,
			Tags:        f.Tags,
			Temporary:   f.Temporary,
			Archived:    f.Archived,
			Config:      cfg,
		}
	}

	segMap := make(map[string]*model.Segment, len(segments))
	for _, seg := range segments {
		segMap[seg.Key] = seg
	}

	snap := &Snapshot{
		EnvironmentKey: envKey,
		ProjectKey:     projectKey,
		Version:        version,
		Flags:          resolvedFlags,
		Segments:       segMap,
		PublishedAt:    time.Now(),
	}

	checksum, err := Checksum(snap)
	if err != nil {
		return nil, err
	}
	snap.Checksum = checksum
	return snap, nil
}
