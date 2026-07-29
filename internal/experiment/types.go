package experiment

import "time"

// Status tracks experiment lifecycle.
type Status string

const (
	StatusDraft    Status = "draft"
	StatusRunning  Status = "running"
	StatusPaused   Status = "paused"
	StatusStopped  Status = "stopped"
	StatusArchived Status = "archived"
)

// Metric describes what to measure.
type Metric struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	EventName   string  `json:"event_name"` // analytics event to count
	Kind        string  `json:"kind"`       // "conversion" | "revenue" | "count"
	IsGuardrail bool    `json:"is_guardrail"`
	MDE         float64 `json:"mde"` // minimum detectable effect (absolute for conversion)
}

// ExperimentVariation in an experiment maps to a flag variation index.
type ExperimentVariation struct {
	VariationIndex int    `json:"variation_index"`
	Name           string `json:"name"`
	IsControl      bool   `json:"is_control"`
	Weight         int    `json:"weight"` // 0-100000
}

// Experiment represents an A/B test tied to a feature flag.
type Experiment struct {
	Key            string                `json:"key"`
	Name           string                `json:"name"`
	Description    string                `json:"description"`
	FlagKey        string                `json:"flag_key"`
	EnvironmentKey string                `json:"environment_key"`
	ProjectKey     string                `json:"project_key"`
	Variations     []ExperimentVariation `json:"variations"`
	Metrics        []Metric              `json:"metrics"`
	Status         Status                `json:"status"`
	Alpha          float64               `json:"alpha"` // significance level, default 0.05
	Power          float64               `json:"power"` // default 0.80
	StartedAt      *time.Time            `json:"started_at"`
	StoppedAt      *time.Time            `json:"stopped_at"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

// ExperimentResults holds computed statistics for an experiment.
type ExperimentResults struct {
	ExperimentKey string          `json:"experiment_key"`
	ComputedAt    time.Time       `json:"computed_at"`
	TotalSamples  int64           `json:"total_samples"`
	SRMResult     *SRMSummary     `json:"srm"`
	MetricResults []*MetricResult `json:"metric_results"`
}

// SRMSummary is a JSON-friendly SRM check result.
type SRMSummary struct {
	ChiSquare float64 `json:"chi_square"`
	PValue    float64 `json:"p_value"`
	Mismatch  bool    `json:"mismatch"`
}

// MetricResult holds statistical results for one metric across all treatment variations.
type MetricResult struct {
	MetricKey  string          `json:"metric_key"`
	MetricName string          `json:"metric_name"`
	Variants   []*VariantMetric `json:"variants"`
}

// VariantMetric holds results for one treatment variation vs control.
type VariantMetric struct {
	VariationIndex    int     `json:"variation_index"`
	Name              string  `json:"name"`
	ControlN          int64   `json:"control_n"`
	ControlConv       int64   `json:"control_conv"`
	TreatmentN        int64   `json:"treatment_n"`
	TreatmentConv     int64   `json:"treatment_conv"`
	ControlRate       float64 `json:"control_rate"`
	TreatmentRate     float64 `json:"treatment_rate"`
	AbsoluteEffect    float64 `json:"absolute_effect"`
	RelativeEffect    float64 `json:"relative_effect"`
	ZScore            float64 `json:"z_score"`
	PValue            float64 `json:"p_value"`
	CILower           float64 `json:"ci_lower"`
	CIUpper           float64 `json:"ci_upper"`
	Significant       bool    `json:"significant"`
	// Sequential testing
	AlwaysValidPValue float64 `json:"always_valid_p_value"`
	SeqDecision       string  `json:"seq_decision"` // "continue" | "reject_null" | "accept_null"
	SamplesRequired   int64   `json:"samples_required"`
}
