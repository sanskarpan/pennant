package store

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"pennant/internal/model"
)

// MemoryStore is an in-process, goroutine-safe implementation of ConfigStore.
// It is intended for tests and local development; it does not persist across restarts.
type MemoryStore struct {
	mu       sync.RWMutex
	projects map[string]*model.Project
	flags    map[string]map[string]*model.Flag    // projectKey -> flagKey -> flag
	segments map[string]map[string]*model.Segment // projectKey -> segKey -> segment
	configs  map[string]map[string]map[string]*model.FlagConfig // proj -> env -> flag -> config
	versions map[string]map[string]*int64                       // proj -> env -> version ptr
	audits   map[string][]*AuditEntry
	auditSeq atomic.Int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		projects: make(map[string]*model.Project),
		flags:    make(map[string]map[string]*model.Flag),
		segments: make(map[string]map[string]*model.Segment),
		configs:  make(map[string]map[string]map[string]*model.FlagConfig),
		versions: make(map[string]map[string]*int64),
		audits:   make(map[string][]*AuditEntry),
	}
}

// --- Projects ---

func (s *MemoryStore) GetProject(key string) (*model.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.projects[key]
	if !ok {
		return nil, fmt.Errorf("project %q not found", key)
	}
	return p, nil
}

func (s *MemoryStore) ListProjects() ([]*model.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Project, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, p)
	}
	return out, nil
}

func (s *MemoryStore) CreateProject(p *model.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.projects[p.Key]; exists {
		return fmt.Errorf("project %q already exists", p.Key)
	}
	s.projects[p.Key] = p
	s.flags[p.Key] = make(map[string]*model.Flag)
	s.segments[p.Key] = make(map[string]*model.Segment)
	s.configs[p.Key] = make(map[string]map[string]*model.FlagConfig)
	s.versions[p.Key] = make(map[string]*int64)
	// Register any environments already embedded in the project
	for _, env := range p.Environments {
		s.configs[p.Key][env.Key] = make(map[string]*model.FlagConfig)
		v := int64(0)
		s.versions[p.Key][env.Key] = &v
	}
	return nil
}

func (s *MemoryStore) UpdateProject(p *model.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.projects[p.Key]; !exists {
		return fmt.Errorf("project %q not found", p.Key)
	}
	s.projects[p.Key] = p
	return nil
}

func (s *MemoryStore) DeleteProject(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.projects, key)
	delete(s.flags, key)
	delete(s.segments, key)
	delete(s.configs, key)
	delete(s.versions, key)
	return nil
}

// --- Environments ---

// GetEnvironment returns a pointer to a copy of the matching environment.
func (s *MemoryStore) GetEnvironment(projectKey, envKey string) (*model.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.projects[projectKey]
	if !ok {
		return nil, fmt.Errorf("project %q not found", projectKey)
	}
	for i := range p.Environments {
		if p.Environments[i].Key == envKey {
			env := p.Environments[i] // copy
			return &env, nil
		}
	}
	return nil, fmt.Errorf("environment %q not found in project %q", envKey, projectKey)
}

func (s *MemoryStore) ListEnvironments(projectKey string) ([]*model.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.projects[projectKey]
	if !ok {
		return nil, fmt.Errorf("project %q not found", projectKey)
	}
	out := make([]*model.Environment, len(p.Environments))
	for i := range p.Environments {
		env := p.Environments[i] // copy
		out[i] = &env
	}
	return out, nil
}

func (s *MemoryStore) CreateEnvironment(projectKey string, env *model.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[projectKey]
	if !ok {
		return fmt.Errorf("project %q not found", projectKey)
	}
	for _, e := range p.Environments {
		if e.Key == env.Key {
			return fmt.Errorf("environment %q already exists in project %q", env.Key, projectKey)
		}
	}
	p.Environments = append(p.Environments, *env)
	if s.configs[projectKey] == nil {
		s.configs[projectKey] = make(map[string]map[string]*model.FlagConfig)
	}
	s.configs[projectKey][env.Key] = make(map[string]*model.FlagConfig)
	if s.versions[projectKey] == nil {
		s.versions[projectKey] = make(map[string]*int64)
	}
	v := int64(0)
	s.versions[projectKey][env.Key] = &v
	return nil
}

func (s *MemoryStore) UpdateEnvironment(projectKey string, env *model.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[projectKey]
	if !ok {
		return fmt.Errorf("project %q not found", projectKey)
	}
	for i := range p.Environments {
		if p.Environments[i].Key == env.Key {
			p.Environments[i] = *env
			return nil
		}
	}
	return fmt.Errorf("environment %q not found in project %q", env.Key, projectKey)
}

func (s *MemoryStore) DeleteEnvironment(projectKey, envKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[projectKey]
	if !ok {
		return fmt.Errorf("project %q not found", projectKey)
	}
	for i, e := range p.Environments {
		if e.Key == envKey {
			p.Environments = append(p.Environments[:i], p.Environments[i+1:]...)
			break
		}
	}
	if s.configs[projectKey] != nil {
		delete(s.configs[projectKey], envKey)
	}
	if s.versions[projectKey] != nil {
		delete(s.versions[projectKey], envKey)
	}
	return nil
}

// --- Flags ---

func (s *MemoryStore) GetFlag(projectKey, flagKey string) (*model.Flag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	flags, ok := s.flags[projectKey]
	if !ok {
		return nil, fmt.Errorf("project %q not found", projectKey)
	}
	f, ok := flags[flagKey]
	if !ok {
		return nil, fmt.Errorf("flag %q not found in project %q", flagKey, projectKey)
	}
	return f, nil
}

func (s *MemoryStore) ListFlags(projectKey string) ([]*model.Flag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	flags, ok := s.flags[projectKey]
	if !ok {
		return nil, fmt.Errorf("project %q not found", projectKey)
	}
	out := make([]*model.Flag, 0, len(flags))
	for _, f := range flags {
		out = append(out, f)
	}
	return out, nil
}

func (s *MemoryStore) CreateFlag(projectKey string, flag *model.Flag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flags[projectKey]; !ok {
		return fmt.Errorf("project %q not found", projectKey)
	}
	if _, exists := s.flags[projectKey][flag.Key]; exists {
		return fmt.Errorf("flag %q already exists in project %q", flag.Key, projectKey)
	}
	now := time.Now()
	flag.CreatedAt = now
	flag.UpdatedAt = now
	s.flags[projectKey][flag.Key] = flag
	return nil
}

func (s *MemoryStore) UpdateFlag(projectKey string, flag *model.Flag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flags[projectKey]; !ok {
		return fmt.Errorf("project %q not found", projectKey)
	}
	if _, exists := s.flags[projectKey][flag.Key]; !exists {
		return fmt.Errorf("flag %q not found in project %q", flag.Key, projectKey)
	}
	flag.UpdatedAt = time.Now()
	s.flags[projectKey][flag.Key] = flag
	return nil
}

func (s *MemoryStore) DeleteFlag(projectKey, flagKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.flags[projectKey]; !ok {
		return fmt.Errorf("project %q not found", projectKey)
	}
	delete(s.flags[projectKey], flagKey)
	return nil
}

// --- FlagConfig ---

func (s *MemoryStore) GetFlagConfig(projectKey, envKey, flagKey string) (*model.FlagConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	envMap, ok := s.configs[projectKey]
	if !ok {
		return nil, fmt.Errorf("project %q not found", projectKey)
	}
	flagMap, ok := envMap[envKey]
	if !ok {
		return nil, fmt.Errorf("environment %q not found in project %q", envKey, projectKey)
	}
	cfg, ok := flagMap[flagKey]
	if !ok {
		return nil, fmt.Errorf("flag config for %q not found in env %q", flagKey, envKey)
	}
	return cfg, nil
}

func (s *MemoryStore) UpsertFlagConfig(projectKey, envKey, flagKey string, cfg *model.FlagConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configs[projectKey] == nil {
		return fmt.Errorf("project %q not found", projectKey)
	}
	if s.configs[projectKey][envKey] == nil {
		s.configs[projectKey][envKey] = make(map[string]*model.FlagConfig)
	}
	s.configs[projectKey][envKey][flagKey] = cfg
	return nil
}

// --- Segments ---

func (s *MemoryStore) GetSegment(projectKey, segKey string) (*model.Segment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	segs, ok := s.segments[projectKey]
	if !ok {
		return nil, fmt.Errorf("project %q not found", projectKey)
	}
	seg, ok := segs[segKey]
	if !ok {
		return nil, fmt.Errorf("segment %q not found in project %q", segKey, projectKey)
	}
	return seg, nil
}

func (s *MemoryStore) ListSegments(projectKey string) ([]*model.Segment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	segs, ok := s.segments[projectKey]
	if !ok {
		return nil, fmt.Errorf("project %q not found", projectKey)
	}
	out := make([]*model.Segment, 0, len(segs))
	for _, seg := range segs {
		out = append(out, seg)
	}
	return out, nil
}

func (s *MemoryStore) CreateSegment(projectKey string, seg *model.Segment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.segments[projectKey]; !ok {
		return fmt.Errorf("project %q not found", projectKey)
	}
	if _, exists := s.segments[projectKey][seg.Key]; exists {
		return fmt.Errorf("segment %q already exists in project %q", seg.Key, projectKey)
	}
	s.segments[projectKey][seg.Key] = seg
	return nil
}

func (s *MemoryStore) UpdateSegment(projectKey string, seg *model.Segment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.segments[projectKey]; !ok {
		return fmt.Errorf("project %q not found", projectKey)
	}
	if _, exists := s.segments[projectKey][seg.Key]; !exists {
		return fmt.Errorf("segment %q not found in project %q", seg.Key, projectKey)
	}
	s.segments[projectKey][seg.Key] = seg
	return nil
}

func (s *MemoryStore) DeleteSegment(projectKey, segKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.segments[projectKey]; !ok {
		return fmt.Errorf("project %q not found", projectKey)
	}
	delete(s.segments[projectKey], segKey)
	return nil
}

// --- Versioning ---

func (s *MemoryStore) GetEnvVersion(projectKey, envKey string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.versions[projectKey] == nil {
		return 0, fmt.Errorf("project %q not found", projectKey)
	}
	v := s.versions[projectKey][envKey]
	if v == nil {
		return 0, fmt.Errorf("environment %q not found in project %q", envKey, projectKey)
	}
	return atomic.LoadInt64(v), nil
}

func (s *MemoryStore) IncrementEnvVersion(projectKey, envKey string) (int64, error) {
	// We need a read lock to get the pointer, then atomic ops on the pointer itself.
	// Using RLock here is safe because we only change the int64 value atomically,
	// not the map structure.
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.versions[projectKey] == nil {
		return 0, fmt.Errorf("project %q not found", projectKey)
	}
	v := s.versions[projectKey][envKey]
	if v == nil {
		return 0, fmt.Errorf("environment %q not found in project %q", envKey, projectKey)
	}
	return atomic.AddInt64(v, 1), nil
}

// --- Audit ---

func (s *MemoryStore) AppendAudit(entry *AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry.ID = fmt.Sprintf("audit-%d", s.auditSeq.Add(1))
	s.audits["global"] = append(s.audits["global"], entry)
	return nil
}

func (s *MemoryStore) ListAudit(projectKey string, limit int) ([]*AuditEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := s.audits["global"]
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return entries, nil
}

// MarshalJSON is a helper used by audit Before/After fields.
func MarshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// compile-time interface check
var _ ConfigStore = (*MemoryStore)(nil)
