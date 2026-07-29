package eval

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"

	"pennant/internal/model"
)

// BucketScale = 2^60 - 1. We take 15 hex chars (60 bits) from SHA-1 digest.
const BucketScale = 0xFFFFFFFFFFFFFFF

// ComputeBucket returns a deterministic float in [0.0, 1.0) for this context.
// Properties: deterministic, uniform, independent per flag, monotone under growth.
// Uses SHA-1 (not MurmurHash) because it's in every language's stdlib with
// byte-identical output — critical for cross-language parity.
func ComputeBucket(ctx *model.Context, bucketBy, key, salt string, seed *int) float64 {
	if bucketBy == "" {
		bucketBy = "key"
	}

	idValue, ok := ctx.GetAttribute(bucketBy)
	if !ok {
		return 0.0
	}

	idStr, ok := stringifyBucketValue(idValue)
	if !ok {
		return 0.0
	}

	var hashInput string
	if seed != nil {
		hashInput = fmt.Sprintf("%d.%s", *seed, idStr)
	} else {
		hashInput = fmt.Sprintf("%s.%s.%s", key, salt, idStr)
	}

	sum := sha1.Sum([]byte(hashInput))
	hexPrefix := hex.EncodeToString(sum[:])[:15]
	n, err := strconv.ParseInt(hexPrefix, 16, 64)
	if err != nil {
		return 0.0
	}
	return float64(n) / float64(BucketScale)
}

// stringifyBucketValue: ONLY strings and integers are valid bucketing values.
// Cross-language parity trap: float and bool string representations differ
// between languages. Integral floats are allowed (JS numbers are all floats).
func stringifyBucketValue(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return strconv.FormatInt(int64(t), 10), true
		}
		return "", false
	default:
		return "", false
	}
}
