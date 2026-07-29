package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStore_Allow(t *testing.T) {
	s := NewStore(10, 5) // 10 rps, burst of 5
	defer s.Close()

	// First 5 requests should all pass (burst)
	allowed := 0
	for i := 0; i < 5; i++ {
		if s.Allow("key1") {
			allowed++
		}
	}
	assert.Equal(t, 5, allowed, "burst of 5 should all be allowed")

	// 6th immediately should be blocked
	assert.False(t, s.Allow("key1"), "6th request should be rate limited")

	// Different key should not be affected
	assert.True(t, s.Allow("key2"), "different key should have its own limit")
}

func TestIPMiddleware_Limits(t *testing.T) {
	handler := IPMiddleware(2, 2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 2 requests pass
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "request %d should pass", i+1)
	}

	// 3rd request limited
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
}

func TestIPMiddleware_SSEExempt(t *testing.T) {
	// Rate limit to 0 (would block everything)
	handler := IPMiddleware(0.001, 0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// SSE path should not be rate limited
	req := httptest.NewRequest("GET", "/sdk/v1/stream", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "SSE path must be exempt from rate limiting")
}

func TestStore_Cleanup(t *testing.T) {
	// Just verify it doesn't panic or deadlock
	s := NewStore(100, 10)
	s.Allow("key1")
	s.Allow("key2")
	time.Sleep(10 * time.Millisecond)
	s.Close()
}
