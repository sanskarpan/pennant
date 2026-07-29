package eval

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	semver "github.com/Masterminds/semver/v3"
	"pennant/internal/model"
)

var regexCache sync.Map

func matchOperator(op model.Operator, attrValue any, clauseValue json.RawMessage) bool {
	switch op {
	case model.OpIn:
		return matchIn(attrValue, clauseValue)
	case model.OpStartsWith:
		return matchStringOp(attrValue, clauseValue, strings.HasPrefix)
	case model.OpEndsWith:
		return matchStringOp(attrValue, clauseValue, strings.HasSuffix)
	case model.OpContains:
		return matchStringOp(attrValue, clauseValue, strings.Contains)
	case model.OpMatches:
		return matchRegex(attrValue, clauseValue)
	case model.OpLessThan:
		return matchNumeric(attrValue, clauseValue, func(a, b float64) bool { return a < b })
	case model.OpLessThanOrEq:
		return matchNumeric(attrValue, clauseValue, func(a, b float64) bool { return a <= b })
	case model.OpGreaterThan:
		return matchNumeric(attrValue, clauseValue, func(a, b float64) bool { return a > b })
	case model.OpGreaterThanOrEq:
		return matchNumeric(attrValue, clauseValue, func(a, b float64) bool { return a >= b })
	case model.OpBefore:
		return matchDatetime(attrValue, clauseValue, func(a, b time.Time) bool { return a.Before(b) })
	case model.OpAfter:
		return matchDatetime(attrValue, clauseValue, func(a, b time.Time) bool { return a.After(b) })
	case model.OpSemVerEqual:
		return matchSemVer(attrValue, clauseValue, func(a, b *semver.Version) bool { return a.Equal(b) })
	case model.OpSemVerLessThan:
		return matchSemVer(attrValue, clauseValue, func(a, b *semver.Version) bool { return a.LessThan(b) })
	case model.OpSemVerGreaterThan:
		return matchSemVer(attrValue, clauseValue, func(a, b *semver.Version) bool { return a.GreaterThan(b) })
	}
	return false
}

func matchIn(attrValue any, clauseValue json.RawMessage) bool {
	var cv any
	if err := json.Unmarshal(clauseValue, &cv); err != nil {
		return false
	}
	return jsonValuesEqual(attrValue, cv)
}

func jsonValuesEqual(a, b any) bool {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case nil:
		return b == nil
	}
	return false
}

func matchStringOp(attrValue any, clauseValue json.RawMessage, fn func(string, string) bool) bool {
	av, ok := attrValue.(string)
	if !ok {
		return false
	}
	var cv string
	if err := json.Unmarshal(clauseValue, &cv); err != nil {
		return false
	}
	return fn(av, cv)
}

func matchRegex(attrValue any, clauseValue json.RawMessage) bool {
	av, ok := attrValue.(string)
	if !ok {
		return false
	}
	var pattern string
	if err := json.Unmarshal(clauseValue, &pattern); err != nil {
		return false
	}
	cached, ok := regexCache.Load(pattern)
	if !ok {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false
		}
		regexCache.Store(pattern, re)
		cached = re
	}
	return cached.(*regexp.Regexp).MatchString(av)
}

func matchNumeric(attrValue any, clauseValue json.RawMessage, fn func(float64, float64) bool) bool {
	av, ok := toFloat64(attrValue)
	if !ok {
		return false
	}
	var cv float64
	if err := json.Unmarshal(clauseValue, &cv); err != nil {
		return false
	}
	return fn(av, cv)
}

func toFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func matchDatetime(attrValue any, clauseValue json.RawMessage, fn func(time.Time, time.Time) bool) bool {
	at := parseTime(attrValue)
	if at == nil {
		return false
	}
	bt := parseTimeFromJSON(clauseValue)
	if bt == nil {
		return false
	}
	return fn(*at, *bt)
}

func parseTime(v any) *time.Time {
	switch t := v.(type) {
	case string:
		pt, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return nil
		}
		return &pt
	case float64:
		pt := time.UnixMilli(int64(t))
		return &pt
	}
	return nil
}

func parseTimeFromJSON(raw json.RawMessage) *time.Time {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		pt, err := time.Parse(time.RFC3339, s)
		if err == nil {
			return &pt
		}
	}
	var ms float64
	if err := json.Unmarshal(raw, &ms); err == nil {
		pt := time.UnixMilli(int64(ms))
		return &pt
	}
	return nil
}

func matchSemVer(attrValue any, clauseValue json.RawMessage, fn func(*semver.Version, *semver.Version) bool) bool {
	av, ok := attrValue.(string)
	if !ok {
		return false
	}
	a, err := semver.NewVersion(av)
	if err != nil {
		return false
	}
	var bs string
	if err := json.Unmarshal(clauseValue, &bs); err != nil {
		return false
	}
	b, err := semver.NewVersion(bs)
	if err != nil {
		return false
	}
	return fn(a, b)
}
