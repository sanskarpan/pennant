package model

type ReasonKind string

const (
	ReasonOff                ReasonKind = "OFF"
	ReasonFallthrough        ReasonKind = "FALLTHROUGH"
	ReasonTargetMatch        ReasonKind = "TARGET_MATCH"
	ReasonRuleMatch          ReasonKind = "RULE_MATCH"
	ReasonPrerequisiteFailed ReasonKind = "PREREQUISITE_FAILED"
	ReasonError              ReasonKind = "ERROR"
)

type ErrorKind string

const (
	ErrFlagNotFound   ErrorKind = "FLAG_NOT_FOUND"
	ErrMalformedFlag  ErrorKind = "MALFORMED_FLAG"
	ErrWrongType      ErrorKind = "WRONG_TYPE"
	ErrClientNotReady ErrorKind = "CLIENT_NOT_READY"
	ErrPrereqCycle    ErrorKind = "PREREQUISITE_CYCLE"
)

type Reason struct {
	Kind            ReasonKind `json:"kind"`
	RuleIndex       *int       `json:"rule_index,omitempty"`
	RuleID          string     `json:"rule_id,omitempty"`
	PrerequisiteKey string     `json:"prerequisite_key,omitempty"`
	ErrorKind       ErrorKind  `json:"error_kind,omitempty"`
	InExperiment    bool       `json:"in_experiment,omitempty"`
}
