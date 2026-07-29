package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"pennant/internal/analytics"
	"pennant/internal/auth"
	"pennant/internal/experiment"
	"pennant/internal/metrics"
	"pennant/internal/ratelimit"
	"pennant/internal/sdkauth"
	"pennant/internal/snapshot"
	"pennant/internal/store"
	"pennant/internal/stream"
)

// Server is the REST API gateway for the feature flag service.
type Server struct {
	store           store.ConfigStore
	builder         *snapshot.Builder
	hub             *stream.Hub
	auth            *sdkauth.Authenticator
	ingestor        *analytics.Ingestor
	adminToken      string
	router          chi.Router
	userStore       auth.UserStorer
	jwtService      *auth.JWTService
	corsOrigins     []string
	experimentStore experiment.ExperimentStore
	engine          *experiment.Engine
}

// NewServer constructs a Server and wires up all routes.
func NewServer(s store.ConfigStore, b *snapshot.Builder, h *stream.Hub, a *sdkauth.Authenticator, ing *analytics.Ingestor, adminToken string, us auth.UserStorer, jwts *auth.JWTService, corsOrigins []string, expStore experiment.ExperimentStore, eng *experiment.Engine) *Server {
	srv := &Server{store: s, builder: b, hub: h, auth: a, ingestor: ing, adminToken: adminToken, userStore: us, jwtService: jwts, corsOrigins: corsOrigins, experimentStore: expStore, engine: eng}
	srv.router = srv.buildRouter()
	return srv
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// ListenAndServe starts the HTTP server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.router)
}

// Shutdown gracefully drains in-flight requests.
func (s *Server) Shutdown(ctx context.Context) error {
	// Chi has no native shutdown; caller should wrap in http.Server and call Shutdown there.
	return nil
}

// jwtMiddleware returns the JWT validation middleware when jwtService is configured,
// or a pass-through middleware that injects an admin claims context when it is nil
// (used in tests and local dev without auth).
func (s *Server) jwtMiddleware() func(http.Handler) http.Handler {
	if s.jwtService != nil {
		return s.jwtService.Middleware
	}
	// Nil jwtService → inject super-admin claims so RequireRole checks pass.
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.InjectClaims(r.Context(), &auth.Claims{
				UserID: "dev",
				Email:  "dev@localhost",
				Role:   auth.RoleAdmin,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(s.corsHeaders)
	r.Use(jsonContentType)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB max
			next.ServeHTTP(w, r)
		})
	})
	r.Use(ratelimit.IPMiddleware(1000, 2000))
	// HTTP instrumentation: record request counts and durations.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			metrics.InstrumentHandler(r.URL.Path, next).ServeHTTP(w, r)
		})
	})

	jwtMW := s.jwtMiddleware()

	// Auth endpoints (no JWT required)
	r.Route("/auth", func(r chi.Router) {
		r.With(ratelimit.LoginMiddleware()).Post("/login", s.authLogin)
		r.Post("/refresh", s.authRefresh)
		// /auth/me requires a valid JWT
		r.Group(func(r chi.Router) {
			r.Use(jwtMW)
			r.Get("/me", s.authMe)
		})
	})

	// Management API — JWT-protected with RBAC
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(jwtMW)

		// Projects — viewer can list/get; editor+ can create/update; admin+ can delete
		r.With(auth.RequireRole(auth.RoleViewer)).Get("/projects", s.listProjects)
		r.With(auth.RequireRole(auth.RoleEditor)).Post("/projects", s.createProject)
		r.Route("/projects/{projectKey}", func(r chi.Router) {
			r.With(auth.RequireRole(auth.RoleViewer)).Get("/", s.getProject)
			r.With(auth.RequireRole(auth.RoleEditor)).Put("/", s.updateProject)
			r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/", s.deleteProject)

			// Audit log
			r.With(auth.RequireRole(auth.RoleViewer)).Get("/audit", s.listAuditLog)

			// Environments
			r.With(auth.RequireRole(auth.RoleViewer)).Get("/environments", s.listEnvironments)
			r.With(auth.RequireRole(auth.RoleEditor)).Post("/environments", s.createEnvironment)
			r.With(auth.RequireRole(auth.RoleEditor)).Put("/environments/{envKey}", s.updateEnvironment)
			r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/environments/{envKey}", s.deleteEnvironment)

			// SDK Key Management
			r.With(auth.RequireRole(auth.RoleViewer)).Get("/environments/{envKey}/sdk-keys", s.listSDKKeys)
			r.With(auth.RequireRole(auth.RoleEditor)).Post("/environments/{envKey}/sdk-keys", s.createSDKKey)
			r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/environments/{envKey}/sdk-keys/{keyValue}", s.revokeSDKKey)

			// Flags
			r.With(auth.RequireRole(auth.RoleViewer)).Get("/flags", s.listFlags)
			r.With(auth.RequireRole(auth.RoleEditor)).Post("/flags", s.createFlag)
			r.Route("/flags/{flagKey}", func(r chi.Router) {
				r.With(auth.RequireRole(auth.RoleViewer)).Get("/", s.getFlag)
				r.With(auth.RequireRole(auth.RoleEditor)).Put("/", s.updateFlag)
				r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/", s.deleteFlag)
				// Per-environment config
				r.With(auth.RequireRole(auth.RoleViewer)).Get("/environments/{envKey}", s.getFlagConfig)
				r.With(auth.RequireRole(auth.RoleEditor)).Put("/environments/{envKey}", s.putFlagConfig)
			})

			// Segments
			r.With(auth.RequireRole(auth.RoleViewer)).Get("/segments", s.listSegments)
			r.With(auth.RequireRole(auth.RoleEditor)).Post("/segments", s.createSegment)
			r.Route("/segments/{segKey}", func(r chi.Router) {
				r.With(auth.RequireRole(auth.RoleViewer)).Get("/", s.getSegment)
				r.With(auth.RequireRole(auth.RoleEditor)).Put("/", s.updateSegment)
				r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/", s.deleteSegment)
			})

			// Experiments
			r.With(auth.RequireRole(auth.RoleViewer)).Get("/experiments", s.listExperiments)
			r.With(auth.RequireRole(auth.RoleEditor)).Post("/experiments", s.createExperiment)
			r.With(auth.RequireRole(auth.RoleViewer)).Get("/experiments/{expKey}/results", s.getExperimentResults)
		})
	})

	// SDK API (authenticated with SDK key)
	r.Route("/sdk/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(ratelimit.SDKKeyMiddleware(500, 1000))
			r.Get("/snapshot", s.sdkSnapshot)
			r.Get("/stream", s.sdkStream)
			r.Post("/evaluate", s.sdkEvaluate)
			r.Post("/track", s.sdkTrack)
		})
	})

	// Health / metrics
	r.Get("/health", s.health)
	r.Get("/ready", s.ready)
	r.Handle("/metrics", promhttp.Handler())

	return r
}

// corsHeaders adds CORS headers. If s.corsOrigins is empty or contains "*",
// all origins are allowed. Otherwise only listed origins are permitted.
func (s *Server) corsHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowAll := len(s.corsOrigins) == 0
		if !allowAll {
			for _, o := range s.corsOrigins {
				if o == "*" {
					allowAll = true
					break
				}
			}
		}
		if allowAll {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			allowed := false
			for _, o := range s.corsOrigins {
				if o == origin {
					allowed = true
					break
				}
			}
			if !allowed && origin != "" {
				http.Error(w, `{"error":"origin not allowed"}`, http.StatusForbidden)
				return
			}
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// jsonContentType sets Content-Type: application/json for all management routes.
func jsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	// Probe the store with a lightweight read
	_, storeErr := s.store.ListProjects()

	status := "ok"
	code := http.StatusOK
	if storeErr != nil {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}

	writeJSON(w, code, map[string]string{
		"status":  status,
		"store":   func() string { if storeErr != nil { return storeErr.Error() }; return "ok" }(),
		"version": "1.0.0",
	})
}

// Kubernetes readiness probe — same as health but separate path
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	_, err := s.store.ListProjects()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}



