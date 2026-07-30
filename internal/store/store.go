package store

import (
	"pennant/internal/model"
)

// ConfigStore is the persistence layer for all flag configuration.
type ConfigStore interface {
	// Projects
	GetProject(key string) (*model.Project, error)
	ListProjects() ([]*model.Project, error)
	CreateProject(p *model.Project) error
	UpdateProject(p *model.Project) error
	DeleteProject(key string) error

	// Environments
	GetEnvironment(projectKey, envKey string) (*model.Environment, error)
	ListEnvironments(projectKey string) ([]*model.Environment, error)
	CreateEnvironment(projectKey string, env *model.Environment) error
	UpdateEnvironment(projectKey string, env *model.Environment) error
	DeleteEnvironment(projectKey, envKey string) error

	// Flags
	GetFlag(projectKey, flagKey string) (*model.Flag, error)
	ListFlags(projectKey string) ([]*model.Flag, error)
	CreateFlag(projectKey string, flag *model.Flag) error
	UpdateFlag(projectKey string, flag *model.Flag) error
	DeleteFlag(projectKey, flagKey string) error

	// FlagConfig (per-environment targeting)
	GetFlagConfig(projectKey, envKey, flagKey string) (*model.FlagConfig, error)
	UpsertFlagConfig(projectKey, envKey, flagKey string, cfg *model.FlagConfig) error

	// Segments
	GetSegment(projectKey, segKey string) (*model.Segment, error)
	ListSegments(projectKey string) ([]*model.Segment, error)
	CreateSegment(projectKey string, seg *model.Segment) error
	UpdateSegment(projectKey string, seg *model.Segment) error
	DeleteSegment(projectKey, segKey string) error

	// Versioning: returns current version for an environment
	GetEnvVersion(projectKey, envKey string) (int64, error)
	IncrementEnvVersion(projectKey, envKey string) (int64, error)

	// Audit
	AppendAudit(entry *AuditEntry) error
	ListAudit(projectKey string, limit int) ([]*AuditEntry, error)
}

type AuditEntry struct {
	ID         string `json:"id"`
	ProjectKey string `json:"project_key"`
	Actor      string `json:"actor"`
	Action     string `json:"action"`
	Resource   string `json:"resource"`
	ResourceID string `json:"resource_id"`
	Before     []byte `json:"before"`
	After      []byte `json:"after"`
	At         int64  `json:"at"` // Unix millis
}
