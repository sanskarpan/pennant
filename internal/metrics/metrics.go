// Package metrics provides Prometheus instrumentation for the pennant service.
package metrics

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the underlying ResponseWriter if it implements http.Flusher.
// Without this, SSE endpoints fail the w.(http.Flusher) assertion.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// InstrumentHandler wraps an http.Handler with Prometheus HTTP metrics recording.
func InstrumentHandler(path string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start).Seconds()
		status := strconv.Itoa(rw.status)
		HTTPRequestsTotal.With(prometheus.Labels{
			"method": r.Method,
			"path":   fmt.Sprintf("%s %s", r.Method, path),
			"status": status,
		}).Inc()
		HTTPRequestDuration.With(prometheus.Labels{
			"method": r.Method,
			"path":   path,
		}).Observe(duration)
	})
}

var (
	// SSE hub
	ActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pennant",
		Name:      "sse_active_connections",
		Help:      "Number of currently active SSE client connections.",
	})
	SlowClientDisconnects = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "pennant",
		Name:      "sse_slow_client_disconnects_total",
		Help:      "Total number of clients disconnected for being too slow.",
	})
	PublishedEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pennant",
		Name:      "sse_published_events_total",
		Help:      "Total SSE events published, by event type.",
	}, []string{"event_type"})

	// Evaluations
	FlagEvaluations = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pennant",
		Name:      "flag_evaluations_total",
		Help:      "Total flag evaluations by flag key and reason.",
	}, []string{"flag_key", "reason"})

	EvalDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "pennant",
		Name:      "flag_eval_duration_seconds",
		Help:      "Flag evaluation latency.",
		Buckets:   prometheus.ExponentialBuckets(0.0001, 2, 12), // 0.1ms → ~400ms
	}, []string{"flag_key"})

	// HTTP
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pennant",
		Name:      "http_requests_total",
		Help:      "Total HTTP requests.",
	}, []string{"method", "path", "status"})

	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "pennant",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request duration.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	// Snapshot
	SnapshotBuildDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "pennant",
		Name:      "snapshot_build_duration_seconds",
		Help:      "Time to build a full environment snapshot.",
		Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10),
	})

	// Analytics
	EventsIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pennant",
		Name:      "events_ingested_total",
		Help:      "Total analytics events ingested by kind.",
	}, []string{"kind"})
)
