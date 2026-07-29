package model

import "encoding/json"

type Operator string

const (
	OpIn                Operator = "in"
	OpEndsWith          Operator = "endsWith"
	OpStartsWith        Operator = "startsWith"
	OpMatches           Operator = "matches"
	OpContains          Operator = "contains"
	OpLessThan          Operator = "lessThan"
	OpLessThanOrEq      Operator = "lessThanOrEqual"
	OpGreaterThan       Operator = "greaterThan"
	OpGreaterThanOrEq   Operator = "greaterThanOrEqual"
	OpBefore            Operator = "before"
	OpAfter             Operator = "after"
	OpSemVerEqual       Operator = "semVerEqual"
	OpSemVerLessThan    Operator = "semVerLessThan"
	OpSemVerGreaterThan Operator = "semVerGreaterThan"
	OpSegmentMatch      Operator = "segmentMatch"
)

type Clause struct {
	Attribute string            `json:"attribute"`
	Op        Operator          `json:"op"`
	Values    []json.RawMessage `json:"values"`
	Negate    bool              `json:"negate"`
}
