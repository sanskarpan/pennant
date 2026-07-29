package experiment

import (
	"fmt"
	"sync"
	"time"
)

// ExperimentStore persists experiments.
type ExperimentStore interface {
	CreateExperiment(exp *Experiment) error
	GetExperiment(key string) (*Experiment, error)
	ListExperiments(projectKey, envKey string) ([]*Experiment, error)
	UpdateExperiment(exp *Experiment) error
	DeleteExperiment(key string) error
	// RecordResults persists computed results for a time-series.
	RecordResults(results *ExperimentResults) error
	GetLatestResults(experimentKey string) (*ExperimentResults, error)
}

// MemExperimentStore is an in-memory store for testing.
type MemExperimentStore struct {
	mu          sync.RWMutex
	experiments map[string]*Experiment
	results     map[string][]*ExperimentResults // experimentKey -> ordered results
}

func NewMemExperimentStore() *MemExperimentStore {
	return &MemExperimentStore{
		experiments: make(map[string]*Experiment),
		results:     make(map[string][]*ExperimentResults),
	}
}

func (s *MemExperimentStore) CreateExperiment(exp *Experiment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.experiments[exp.Key]; exists {
		return fmt.Errorf("experiment %q already exists", exp.Key)
	}
	exp.CreatedAt = time.Now()
	exp.UpdatedAt = time.Now()
	s.experiments[exp.Key] = exp
	return nil
}

func (s *MemExperimentStore) GetExperiment(key string) (*Experiment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exp, ok := s.experiments[key]
	if !ok {
		return nil, fmt.Errorf("experiment %q not found", key)
	}
	return exp, nil
}

func (s *MemExperimentStore) ListExperiments(projectKey, envKey string) ([]*Experiment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Experiment
	for _, exp := range s.experiments {
		if exp.ProjectKey == projectKey && exp.EnvironmentKey == envKey {
			out = append(out, exp)
		}
	}
	return out, nil
}

func (s *MemExperimentStore) UpdateExperiment(exp *Experiment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.experiments[exp.Key]; !exists {
		return fmt.Errorf("experiment %q not found", exp.Key)
	}
	exp.UpdatedAt = time.Now()
	s.experiments[exp.Key] = exp
	return nil
}

func (s *MemExperimentStore) DeleteExperiment(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.experiments, key)
	return nil
}

func (s *MemExperimentStore) RecordResults(results *ExperimentResults) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[results.ExperimentKey] = append(s.results[results.ExperimentKey], results)
	return nil
}

func (s *MemExperimentStore) GetLatestResults(key string) (*ExperimentResults, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rs := s.results[key]
	if len(rs) == 0 {
		return nil, fmt.Errorf("no results found for experiment %q", key)
	}
	return rs[len(rs)-1], nil
}
