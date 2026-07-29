package analytics

import "encoding/json"

// EventKind identifies the type of analytics event.
type EventKind string

const (
	// EvalEvent: recorded when a flag is evaluated.
	EvalEvent EventKind = "eval"
	// CustomEvent: user-defined event (e.g. "purchase", "signup") with optional metric value.
	CustomEvent EventKind = "custom"
	// ExposureEvent: user was exposed to an experiment (subset of evals with isExperiment=true).
	ExposureEvent EventKind = "exposure"
)

// Event is a single analytics data point.
type Event struct {
	Kind           EventKind       `json:"kind"`
	FlagKey        string          `json:"flag_key,omitempty"`
	EnvironmentKey string          `json:"environment_key"`
	ProjectKey     string          `json:"project_key"`
	ContextKey     string          `json:"context_key"`
	ContextKind    string          `json:"context_kind"`
	VariationIndex *int            `json:"variation_index,omitempty"`
	// ReasonKind: "OFF", "FALLTHROUGH", "RULE_MATCH", etc.
	ReasonKind     string          `json:"reason_kind,omitempty"`
	MetricName     string          `json:"metric_name,omitempty"`
	MetricValue    float64         `json:"metric_value,omitempty"`
	ExperimentKey  string          `json:"experiment_key,omitempty"`
	Timestamp      int64           `json:"timestamp"` // Unix millis
	Properties     json.RawMessage `json:"properties,omitempty"`
}

// FlagInsight aggregates eval counts and conversion data for a single flag+variation.
type FlagInsight struct {
	FlagKey         string           `json:"flag_key"`
	EnvironmentKey  string           `json:"environment_key"`
	VariationCounts map[string]int64 `json:"variation_counts"` // variation_index_str -> count
	TotalEvals      int64            `json:"total_evals"`
	UniqueContexts  int64            `json:"unique_contexts"`
}

// StaleFlag is emitted by the stale detection pass.
type StaleFlag struct {
	FlagKey        string `json:"flag_key"`
	EnvironmentKey string `json:"environment_key"`
	LastEvalAt     int64  `json:"last_eval_at"` // Unix millis; 0 if never evaluated
	Reason         string `json:"reason"`       // "no_evals_30d", "single_variation", "permanent_off"
}
