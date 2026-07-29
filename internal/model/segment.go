package model

import "fmt"

type Segment struct {
	Key         string        `json:"key"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Included    []string      `json:"included"`
	Excluded    []string      `json:"excluded"`
	Rules       []SegmentRule `json:"rules"`
	Salt        string        `json:"salt"`
	Version     int64         `json:"version"`
}

type SegmentRule struct {
	ID       string   `json:"id"`
	Clauses  []Clause `json:"clauses"`
	Weight   *int     `json:"weight,omitempty"`
	BucketBy string   `json:"bucket_by"`
}

func (s *Segment) Validate() error {
	excl := make(map[string]bool, len(s.Excluded))
	for _, k := range s.Excluded {
		excl[k] = true
	}
	for _, k := range s.Included {
		if excl[k] {
			return fmt.Errorf("segment %q: key %q is in both included and excluded", s.Key, k)
		}
	}
	return nil
}
