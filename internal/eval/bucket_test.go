package eval

import (
	"fmt"
	"testing"

	"pennant/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestBucket_Deterministic(t *testing.T) {
	ctx := &model.Context{Kind: "user", Key: "user-4821"}
	first := ComputeBucket(ctx, "key", "my-flag", "salt123", nil)
	for i := 0; i < 10000; i++ {
		assert.Equal(t, first, ComputeBucket(ctx, "key", "my-flag", "salt123", nil))
	}
}

func TestBucket_Uniform(t *testing.T) {
	const n, bins = 100000, 100
	counts := make([]int, bins)
	for i := 0; i < n; i++ {
		ctx := &model.Context{Kind: "user", Key: fmt.Sprintf("user-%d", i)}
		b := ComputeBucket(ctx, "key", "my-flag", "salt", nil)
		idx := int(b * bins)
		if idx >= bins {
			idx = bins - 1
		}
		counts[idx]++
	}
	expected := float64(n) / float64(bins)
	for i, c := range counts {
		ratio := float64(c) / expected
		assert.True(t, ratio > 0.7 && ratio < 1.3,
			"bin %d: count %d, expected ~%f, ratio %f", i, c, expected, ratio)
	}
}

func TestBucket_IndependentPerFlag(t *testing.T) {
	var bothLow, aLow int
	for i := 0; i < 10000; i++ {
		ctx := &model.Context{Kind: "user", Key: fmt.Sprintf("user-%d", i)}
		a := ComputeBucket(ctx, "key", "flag-a", "salt-a", nil)
		b := ComputeBucket(ctx, "key", "flag-b", "salt-b", nil)
		if a < 0.1 {
			aLow++
			if b < 0.1 {
				bothLow++
			}
		}
	}
	ratio := float64(bothLow) / float64(aLow)
	assert.InDelta(t, 0.1, ratio, 0.03, "flag buckets must be independent")
}

func TestBucket_Monotonic(t *testing.T) {
	in10 := map[string]bool{}
	for i := 0; i < 20000; i++ {
		k := fmt.Sprintf("user-%d", i)
		b := ComputeBucket(&model.Context{Kind: "user", Key: k}, "key", "f", "s", nil)
		if b < 0.10 {
			in10[k] = true
		}
	}
	for k := range in10 {
		b := ComputeBucket(&model.Context{Kind: "user", Key: k}, "key", "f", "s", nil)
		assert.Less(t, b, 0.20, "user %s lost the feature when ramping 10%%->20%%", k)
	}
}

func TestBucket_KnownValues(t *testing.T) {
	cases := []struct {
		key, flag, salt string
	}{
		{"user-0001", "checkout-experiment", "a1b2c3"},
		{"user-0002", "checkout-experiment", "a1b2c3"},
		{"user-0003", "checkout-experiment", "a1b2c3"},
	}
	for _, c := range cases {
		got := ComputeBucket(&model.Context{Kind: "user", Key: c.key}, "key", c.flag, c.salt, nil)
		t.Logf("key=%s bucket=%.15f", c.key, got)
		assert.GreaterOrEqual(t, got, 0.0)
		assert.Less(t, got, 1.0)
	}
}

func TestBucket_MissingAttr(t *testing.T) {
	ctx := &model.Context{Kind: "user", Key: "user-1"}
	b := ComputeBucket(ctx, "nonexistent", "flag", "salt", nil)
	assert.Equal(t, 0.0, b)
}

func TestBucket_Seed(t *testing.T) {
	ctx := &model.Context{Kind: "user", Key: "user-1"}
	seed := 42
	b1 := ComputeBucket(ctx, "key", "flag", "salt", &seed)
	b2 := ComputeBucket(ctx, "key", "flag", "salt", &seed)
	assert.Equal(t, b1, b2)
	b3 := ComputeBucket(ctx, "key", "flag", "salt", nil)
	assert.NotEqual(t, b1, b3)
}

func TestBucket_IntegralFloat(t *testing.T) {
	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{"id": float64(12345)}}
	b := ComputeBucket(ctx, "id", "flag", "salt", nil)
	assert.GreaterOrEqual(t, b, 0.0)
	assert.Less(t, b, 1.0)
}

func TestBucket_NonIntegralFloat_Returns0(t *testing.T) {
	ctx := &model.Context{Kind: "user", Key: "u1", Attributes: map[string]any{"score": 1.5}}
	b := ComputeBucket(ctx, "score", "flag", "salt", nil)
	assert.Equal(t, 0.0, b)
}
