package snapshot

import (
	"pennant/internal/model"
	"time"
)

// Snapshot is a versioned, checksummed view of all flag configs for one environment.
type Snapshot struct {
	EnvironmentKey string                    `json:"environment_key"`
	ProjectKey     string                    `json:"project_key"`
	Version        int64                     `json:"version"`
	Flags          map[string]*ResolvedFlag  `json:"flags"`
	Segments       map[string]*model.Segment `json:"segments"`
	Checksum       string                    `json:"checksum"`
	PublishedAt    time.Time                 `json:"published_at"`
}

// ResolvedFlag merges Flag metadata with its environment-specific FlagConfig.
type ResolvedFlag struct {
	Key         string              `json:"key"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Type        model.VariationType `json:"type"`
	Variations  []model.Variation   `json:"variations"`
	Tags        []string            `json:"tags"`
	Temporary   bool                `json:"temporary"`
	Archived    bool                `json:"archived"`
	Config      *model.FlagConfig   `json:"config"`
}

// Delta is sent when the client's Last-Event-ID is recent enough to apply a patch
// rather than receiving the full snapshot.
type Delta struct {
	EnvironmentKey   string            `json:"environment_key"`
	ProjectKey       string            `json:"project_key"`
	FromVersion      int64             `json:"from_version"`
	ToVersion        int64             `json:"to_version"`
	UpsertedFlags    []*ResolvedFlag   `json:"upserted_flags"`
	DeletedFlags     []string          `json:"deleted_flags"`
	UpsertedSegments []*model.Segment  `json:"upserted_segments"`
	DeletedSegments  []string          `json:"deleted_segments"`
	Checksum         string            `json:"checksum"` // checksum of the resulting snapshot
}
