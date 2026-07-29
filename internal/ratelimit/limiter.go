package ratelimit

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// entry wraps a rate limiter with a last-seen timestamp for cleanup.
type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// Store holds per-key rate limiters with background cleanup.
type Store struct {
	mu      sync.Mutex
	entries map[string]*entry
	rps     rate.Limit
	burst   int
	done    chan struct{}
}

// NewStore creates a rate limiter store. rps is requests per second, burst is max burst.
func NewStore(rps float64, burst int) *Store {
	s := &Store{
		entries: make(map[string]*entry),
		rps:     rate.Limit(rps),
		burst:   burst,
		done:    make(chan struct{}),
	}
	go s.cleanup()
	return s
}

// Close stops the background cleanup goroutine.
func (s *Store) Close() { close(s.done) }

func (s *Store) getLimiter(key string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(s.rps, s.burst)}
		s.entries[key] = e
	}
	e.lastSeen = time.Now()
	return e.limiter
}

// Allow returns true if the key is within rate limit.
func (s *Store) Allow(key string) bool {
	return s.getLimiter(key).Allow()
}

// cleanup removes stale entries every minute.
func (s *Store) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			for k, e := range s.entries {
				if time.Since(e.lastSeen) > 5*time.Minute {
					delete(s.entries, k)
				}
			}
			s.mu.Unlock()
		}
	}
}

// IPMiddleware limits requests by client IP.
func IPMiddleware(rps float64, burst int) func(http.Handler) http.Handler {
	store := NewStore(rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip SSE endpoints — they're long-lived connections, not per-request
			if r.URL.Path == "/sdk/v1/stream" {
				next.ServeHTTP(w, r)
				return
			}
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			// Respect X-Real-IP from reverse proxies
			if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
				ip = realIP
			}
			if !store.Allow(ip) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SDKKeyMiddleware limits requests by SDK key (for /sdk/v1/* endpoints).
func SDKKeyMiddleware(rps float64, burst int) func(http.Handler) http.Handler {
	store := NewStore(rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip SSE stream endpoint
			if r.URL.Path == "/sdk/v1/stream" {
				next.ServeHTTP(w, r)
				return
			}
			key := extractSDKKey(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !store.Allow(key) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"SDK key rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractSDKKey(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && h[:7] == "Bearer " {
		return h[7:]
	}
	return r.URL.Query().Get("sdkKey")
}

// LoginMiddleware limits login attempts to 5 per minute per IP.
func LoginMiddleware() func(http.Handler) http.Handler {
	store := NewStore(float64(5)/60, 5) // 5 per minute, burst 5
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
				ip = realIP
			}
			if !store.Allow(ip) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, `{"error":"too many login attempts, try again in 60 seconds"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
