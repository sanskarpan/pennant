package analytics

import (
	"fmt"
	"sync"
	"time"
)

// MemEventStore is an in-memory EventStore for testing and development.
type MemEventStore struct {
	mu     sync.RWMutex
	events []*Event
}

func NewMemEventStore() *MemEventStore {
	return &MemEventStore{}
}

func (s *MemEventStore) InsertEvents(events []*Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, events...)
	return nil
}

func (s *MemEventStore) QueryFlagInsights(projectKey, envKey string, since int64) ([]*FlagInsight, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byFlag := make(map[string]*FlagInsight)
	seenContexts := make(map[string]map[string]bool) // flagKey -> contextKey -> seen

	for _, e := range s.events {
		if e.Kind != EvalEvent {
			continue
		}
		if e.EnvironmentKey != envKey || e.ProjectKey != projectKey {
			continue
		}
		if e.Timestamp < since {
			continue
		}
		fi, ok := byFlag[e.FlagKey]
		if !ok {
			fi = &FlagInsight{
				FlagKey:         e.FlagKey,
				EnvironmentKey:  e.EnvironmentKey,
				VariationCounts: make(map[string]int64),
			}
			byFlag[e.FlagKey] = fi
			seenContexts[e.FlagKey] = make(map[string]bool)
		}
		fi.TotalEvals++
		if e.VariationIndex != nil {
			key := fmt.Sprintf("%d", *e.VariationIndex)
			fi.VariationCounts[key]++
		}
		if !seenContexts[e.FlagKey][e.ContextKey] {
			seenContexts[e.FlagKey][e.ContextKey] = true
			fi.UniqueContexts++
		}
	}

	out := make([]*FlagInsight, 0, len(byFlag))
	for _, fi := range byFlag {
		out = append(out, fi)
	}
	return out, nil
}

func (s *MemEventStore) QueryStaleFlags(projectKey, envKey string) ([]*StaleFlag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	const staleDays = 30
	staleThreshold := time.Now().Add(-staleDays * 24 * time.Hour).UnixMilli()

	lastEval := make(map[string]int64)          // flagKey -> last eval timestamp
	variationsSeen := make(map[string]map[int]bool) // flagKey -> set of variation indices seen
	offOnly := make(map[string]bool)

	for _, e := range s.events {
		if e.Kind != EvalEvent {
			continue
		}
		if e.EnvironmentKey != envKey || e.ProjectKey != projectKey {
			continue
		}
		if e.Timestamp > lastEval[e.FlagKey] {
			lastEval[e.FlagKey] = e.Timestamp
		}
		if variationsSeen[e.FlagKey] == nil {
			variationsSeen[e.FlagKey] = make(map[int]bool)
		}
		if e.VariationIndex != nil {
			variationsSeen[e.FlagKey][*e.VariationIndex] = true
		}
		if e.ReasonKind == "OFF" {
			offOnly[e.FlagKey] = true
		} else {
			offOnly[e.FlagKey] = false
		}
	}

	var stale []*StaleFlag
	for flagKey, lastAt := range lastEval {
		sf := &StaleFlag{
			FlagKey:        flagKey,
			EnvironmentKey: envKey,
			LastEvalAt:     lastAt,
		}
		if lastAt < staleThreshold {
			sf.Reason = "no_evals_30d"
			stale = append(stale, sf)
		} else if len(variationsSeen[flagKey]) <= 1 {
			sf.Reason = "single_variation"
			stale = append(stale, sf)
		}
	}
	return stale, nil
}

func (s *MemEventStore) QueryConversions(projectKey, envKey, flagKey, metricEvent string, since int64) (map[int]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// First, build a map of contextKey -> variationIndex from eval events
	ctxToVar := make(map[string]int)
	for _, e := range s.events {
		if e.Kind != EvalEvent || e.FlagKey != flagKey ||
			e.EnvironmentKey != envKey || e.ProjectKey != projectKey {
			continue
		}
		if e.VariationIndex != nil {
			ctxToVar[e.ContextKey] = *e.VariationIndex
		}
	}

	// Then count custom metric events, bucketed by the user's variation
	result := make(map[int]int64)
	for _, e := range s.events {
		if e.Kind != CustomEvent || e.MetricName != metricEvent ||
			e.EnvironmentKey != envKey || e.ProjectKey != projectKey ||
			e.Timestamp < since {
			continue
		}
		varIdx, ok := ctxToVar[e.ContextKey]
		if !ok {
			continue // user not exposed to this flag
		}
		result[varIdx]++
	}
	return result, nil
}

// All returns all stored events (for testing).
func (s *MemEventStore) All() []*Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Event, len(s.events))
	copy(out, s.events)
	return out
}
