package snapshot

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// CanonicalJSON produces deterministic JSON: object keys sorted lexicographically,
// no whitespace, shortest round-trip numbers. Both the Go server and any SDK must
// produce identical bytes for the same logical snapshot — this is what checksum
// correctness requires.
func CanonicalJSON(v any) ([]byte, error) {
	// Marshal then unmarshal to normalize all types to generic Go values
	// (e.g. time.Time -> string, json.RawMessage -> any), then re-encode
	// with sorted keys.
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, err
	}
	return marshalSorted(normalized)
}

func marshalSorted(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		result := []byte{'{'}
		for i, k := range keys {
			keyBytes, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			valBytes, err := marshalSorted(t[k])
			if err != nil {
				return nil, err
			}
			if i > 0 {
				result = append(result, ',')
			}
			result = append(result, keyBytes...)
			result = append(result, ':')
			result = append(result, valBytes...)
		}
		result = append(result, '}')
		return result, nil

	case []any:
		result := []byte{'['}
		for i, elem := range t {
			if i > 0 {
				result = append(result, ',')
			}
			elemBytes, err := marshalSorted(elem)
			if err != nil {
				return nil, err
			}
			result = append(result, elemBytes...)
		}
		result = append(result, ']')
		return result, nil

	default:
		return json.Marshal(v)
	}
}

// Checksum returns the SHA-256 of the canonical JSON of the snapshot's content
// (excluding the checksum field itself), prefixed with "sha256:".
func Checksum(snap *Snapshot) (string, error) {
	// Convert segments to a map[string]any for canonical encoding alongside flags.
	segsAny := make(map[string]any, len(snap.Segments))
	for k, v := range snap.Segments {
		segsAny[k] = v
	}

	payload := struct {
		EnvironmentKey string                   `json:"environment_key"`
		ProjectKey     string                   `json:"project_key"`
		Version        int64                    `json:"version"`
		Flags          map[string]*ResolvedFlag `json:"flags"`
		Segments       map[string]any           `json:"segments"`
	}{
		EnvironmentKey: snap.EnvironmentKey,
		ProjectKey:     snap.ProjectKey,
		Version:        snap.Version,
		Flags:          snap.Flags,
		Segments:       segsAny,
	}

	b, err := CanonicalJSON(payload)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("sha256:%x", h), nil
}
