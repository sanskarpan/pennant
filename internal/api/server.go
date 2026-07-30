package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"pennant/internal/analytics"
	"pennant/internal/auth"
	"pennant/internal/eval"
	"pennant/internal/experiment"
	"pennant/internal/metrics"
	"pennant/internal/model"
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

// PaginatedResponse is the envelope returned by paginated list endpoints.
type PaginatedResponse[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// parsePagination parses ?limit= and ?offset= from the request query.
// Default limit is 50; maximum is 500.
func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

// applyPagination slices a slice according to limit/offset and returns the page
// and total count before slicing.
func applyPagination[T any](items []T, limit, offset int) ([]T, int) {
	total := len(items)
	if offset >= total {
		return []T{}, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return items[offset:end], total
}

// ---- Auth endpoints ----

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, ok := s.userStore.GetByEmail(req.Email)
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	accessToken, err := s.jwtService.Issue(auth.Claims{
		UserID: u.ID,
		Email:  u.Email,
		Name:   u.Name,
		Role:   u.Role,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	refreshToken := s.userStore.IssueRefreshToken(u.ID)
	writeJSON(w, http.StatusOK, auth.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    3600,
		TokenType:    "Bearer",
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (s *Server) authRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userID, ok := s.userStore.ValidateRefreshToken(req.RefreshToken)
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}
	u, ok := s.userStore.GetByID(userID)
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not found")
		return
	}
	accessToken, err := s.jwtService.Issue(auth.Claims{
		UserID: u.ID,
		Email:  u.Email,
		Name:   u.Name,
		Role:   u.Role,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	newRefresh := s.userStore.IssueRefreshToken(u.ID)
	writeJSON(w, http.StatusOK, auth.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: newRefresh,
		ExpiresIn:    3600,
		TokenType:    "Bearer",
	})
}

func (s *Server) authMe(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r)
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	u, ok := s.userStore.GetByID(claims.UserID)
	if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// ---- Projects ----

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.ListProjects()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	limit, offset := parsePagination(r)
	page, total := applyPagination(projects, limit, offset)
	writeJSON(w, http.StatusOK, PaginatedResponse[*model.Project]{
		Items:  page,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var p model.Project
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if p.Key == "" {
		writeError(w, http.StatusBadRequest, "project key is required")
		return
	}
	if err := s.store.CreateProject(&p); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.appendAudit(r, "create", "project", p.Key, nil, &p)
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "projectKey")
	p, err := s.store.GetProject(key)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "projectKey")
	before, _ := s.store.GetProject(key)
	var p model.Project
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p.Key = key
	if err := s.store.UpdateProject(&p); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.appendAudit(r, "update", "project", key, before, &p)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "projectKey")
	before, _ := s.store.GetProject(key)
	if err := s.store.DeleteProject(key); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.appendAudit(r, "delete", "project", key, before, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---- Environments ----

func (s *Server) listEnvironments(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	envs, err := s.store.ListEnvironments(projectKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envs)
}

func (s *Server) createEnvironment(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	var env model.Environment
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if env.Key == "" {
		writeError(w, http.StatusBadRequest, "environment key is required")
		return
	}
	env.ProjectKey = projectKey
	if err := s.store.CreateEnvironment(projectKey, &env); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.appendAudit(r, "create", "environment", env.Key, nil, &env)
	writeJSON(w, http.StatusCreated, env)
}

func (s *Server) updateEnvironment(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	envKey := chi.URLParam(r, "envKey")
	before, _ := s.store.GetEnvironment(projectKey, envKey)
	var env model.Environment
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	env.Key = envKey
	env.ProjectKey = projectKey
	if err := s.store.UpdateEnvironment(projectKey, &env); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.appendAudit(r, "update", "environment", envKey, before, &env)
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) deleteEnvironment(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	envKey := chi.URLParam(r, "envKey")
	before, _ := s.store.GetEnvironment(projectKey, envKey)
	if err := s.store.DeleteEnvironment(projectKey, envKey); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.appendAudit(r, "delete", "environment", envKey, before, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---- SDK Keys ----

func (s *Server) listSDKKeys(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	envKey := chi.URLParam(r, "envKey")
	env, err := s.store.GetEnvironment(projectKey, envKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, env.SDKKeys)
}

type createSDKKeyRequest struct {
	Type string `json:"type"` // "server", "client", "mobile"
}

func (s *Server) createSDKKey(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	envKey := chi.URLParam(r, "envKey")
	var req createSDKKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Type == "" {
		req.Type = "server"
	}
	env, err := s.store.GetEnvironment(projectKey, envKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	newKey := model.SDKKey{
		Value:     fmt.Sprintf("sdk-%s-%s-%d", req.Type, envKey, time.Now().UnixNano()),
		Type:      model.SDKKeyType(req.Type),
		CreatedAt: time.Now(),
	}
	env.SDKKeys = append(env.SDKKeys, newKey)
	if err := s.store.UpdateEnvironment(projectKey, env); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.auth != nil {
		s.auth.Register(&sdkauth.SDKKeyRecord{
			Value:      newKey.Value,
			ProjectKey: projectKey,
			EnvKey:     envKey,
			Type:       req.Type,
		})
	}
	writeJSON(w, http.StatusCreated, newKey)
}

func (s *Server) revokeSDKKey(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	envKey := chi.URLParam(r, "envKey")
	keyValue := chi.URLParam(r, "keyValue")
	env, err := s.store.GetEnvironment(projectKey, envKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	newKeys := make([]model.SDKKey, 0, len(env.SDKKeys))
	found := false
	for _, k := range env.SDKKeys {
		if k.Value == keyValue {
			found = true
			continue
		}
		newKeys = append(newKeys, k)
	}
	if !found {
		writeError(w, http.StatusNotFound, "sdk key not found")
		return
	}
	env.SDKKeys = newKeys
	if err := s.store.UpdateEnvironment(projectKey, env); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.auth != nil {
		s.auth.Revoke(keyValue)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Flags ----

func (s *Server) listFlags(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	flags, err := s.store.ListFlags(projectKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	limit, offset := parsePagination(r)
	page, total := applyPagination(flags, limit, offset)
	writeJSON(w, http.StatusOK, PaginatedResponse[*model.Flag]{
		Items:  page,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (s *Server) createFlag(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	var flag model.Flag
	if err := json.NewDecoder(r.Body).Decode(&flag); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if flag.Key == "" {
		writeError(w, http.StatusBadRequest, "flag key is required")
		return
	}
	if err := flag.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.CreateFlag(projectKey, &flag); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.appendAudit(r, "create", "flag", flag.Key, nil, &flag)
	s.publishFlagChange(r, projectKey, &flag)
	writeJSON(w, http.StatusCreated, flag)
}

func (s *Server) getFlag(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	flagKey := chi.URLParam(r, "flagKey")
	flag, err := s.store.GetFlag(projectKey, flagKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, flag)
}

func (s *Server) updateFlag(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	flagKey := chi.URLParam(r, "flagKey")
	before, _ := s.store.GetFlag(projectKey, flagKey)
	var flag model.Flag
	if err := json.NewDecoder(r.Body).Decode(&flag); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	flag.Key = flagKey
	if err := flag.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateFlag(projectKey, &flag); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.appendAudit(r, "update", "flag", flagKey, before, &flag)
	s.publishFlagChange(r, projectKey, &flag)
	writeJSON(w, http.StatusOK, flag)
}

func (s *Server) deleteFlag(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	flagKey := chi.URLParam(r, "flagKey")
	before, _ := s.store.GetFlag(projectKey, flagKey)
	if err := s.store.DeleteFlag(projectKey, flagKey); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.appendAudit(r, "delete", "flag", flagKey, before, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getFlagConfig(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	flagKey := chi.URLParam(r, "flagKey")
	envKey := chi.URLParam(r, "envKey")
	cfg, err := s.store.GetFlagConfig(projectKey, envKey, flagKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) putFlagConfig(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	flagKey := chi.URLParam(r, "flagKey")
	envKey := chi.URLParam(r, "envKey")
	var cfg model.FlagConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.store.UpsertFlagConfig(projectKey, envKey, flagKey, &cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Bump version and publish SSE update
	newVersion, _ := s.store.IncrementEnvVersion(projectKey, envKey)
	snap, err := s.builder.Build(projectKey, envKey)
	if err == nil {
		s.hub.Publish(envKey, stream.Message{
			Event:   "put",
			Version: newVersion,
			Data:    snap,
		})
	}
	writeJSON(w, http.StatusOK, cfg)
}

// ---- Segments ----

func (s *Server) listSegments(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	segs, err := s.store.ListSegments(projectKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	limit, offset := parsePagination(r)
	page, total := applyPagination(segs, limit, offset)
	writeJSON(w, http.StatusOK, PaginatedResponse[*model.Segment]{
		Items:  page,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func (s *Server) createSegment(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	var seg model.Segment
	if err := json.NewDecoder(r.Body).Decode(&seg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if seg.Key == "" {
		writeError(w, http.StatusBadRequest, "segment key is required")
		return
	}
	if err := seg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.CreateSegment(projectKey, &seg); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.appendAudit(r, "create", "segment", seg.Key, nil, &seg)
	writeJSON(w, http.StatusCreated, seg)
}

func (s *Server) getSegment(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	segKey := chi.URLParam(r, "segKey")
	seg, err := s.store.GetSegment(projectKey, segKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, seg)
}

func (s *Server) updateSegment(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	segKey := chi.URLParam(r, "segKey")
	before, _ := s.store.GetSegment(projectKey, segKey)
	var seg model.Segment
	if err := json.NewDecoder(r.Body).Decode(&seg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	seg.Key = segKey
	if err := seg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateSegment(projectKey, &seg); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.appendAudit(r, "update", "segment", segKey, before, &seg)
	writeJSON(w, http.StatusOK, seg)
}

func (s *Server) deleteSegment(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	segKey := chi.URLParam(r, "segKey")
	before, _ := s.store.GetSegment(projectKey, segKey)
	if err := s.store.DeleteSegment(projectKey, segKey); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.appendAudit(r, "delete", "segment", segKey, before, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---- Experiments ----

func (s *Server) listExperiments(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	envKey := r.URL.Query().Get("env")
	if s.experimentStore == nil {
		writeJSON(w, http.StatusOK, []*experiment.Experiment{})
		return
	}
	exps, err := s.experimentStore.ListExperiments(projectKey, envKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, exps)
}

func (s *Server) createExperiment(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	if s.experimentStore == nil {
		writeError(w, http.StatusServiceUnavailable, "experiment store not configured")
		return
	}
	var exp experiment.Experiment
	if err := json.NewDecoder(r.Body).Decode(&exp); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	exp.ProjectKey = projectKey
	if err := s.experimentStore.CreateExperiment(&exp); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, exp)
}

func (s *Server) getExperimentResults(w http.ResponseWriter, r *http.Request) {
	expKey := chi.URLParam(r, "expKey")
	if s.experimentStore == nil {
		writeError(w, http.StatusServiceUnavailable, "experiment store not configured")
		return
	}
	results, err := s.experimentStore.GetLatestResults(expKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// ---- Audit ----

func (s *Server) listAuditLog(w http.ResponseWriter, r *http.Request) {
	projectKey := chi.URLParam(r, "projectKey")
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := s.store.ListAudit(projectKey, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// ---- SDK endpoints ----

func (s *Server) sdkSnapshot(w http.ResponseWriter, r *http.Request) {
	rec := s.authenticateSDK(w, r)
	if rec == nil {
		return
	}
	snap, err := s.builder.Build(rec.ProjectKey, rec.EnvKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.SnapshotBuildDuration.Observe(0)
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) sdkStream(w http.ResponseWriter, r *http.Request) {
	rec := s.authenticateSDK(w, r)
	if rec == nil {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	metrics.ActiveConnections.Inc()
	defer metrics.ActiveConnections.Dec()

	// Determine last-seen version from client header
	var fromVersion int64
	if leid := r.Header.Get("Last-Event-ID"); leid != "" {
		fmt.Sscanf(leid, "%d", &fromVersion)
	}

	// Try to replay missed events
	if msgs, ok := s.hub.Replay(rec.EnvKey, fromVersion); ok && len(msgs) > 0 {
		for _, msg := range msgs {
			data, _ := json.Marshal(msg.Data)
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", msg.Version, msg.Event, data)
			flusher.Flush()
		}
	} else {
		// Send full snapshot
		snap, err := s.builder.Build(rec.ProjectKey, rec.EnvKey)
		if err == nil {
			data, _ := json.Marshal(snap)
			version, _ := s.store.GetEnvVersion(rec.ProjectKey, rec.EnvKey)
			fmt.Fprintf(w, "id: %d\nevent: put\ndata: %s\n\n", version, data)
			flusher.Flush()
		}
	}

	sub := s.hub.Subscribe(rec.EnvKey, rec.ProjectKey, rec.Value, r.UserAgent())
	defer s.hub.Unsubscribe(rec.EnvKey, sub.ID)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, open := <-sub.Ch:
			if !open {
				return
			}
			data, _ := json.Marshal(msg.Data)
			_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", msg.Version, msg.Event, data)
			if err != nil {
				metrics.SlowClientDisconnects.Inc()
				return
			}
			flusher.Flush()
		}
	}
}

type evaluateRequest struct {
	FlagKey string        `json:"flag_key"`
	Context model.Context `json:"context"`
}

type evaluateResponse struct {
	FlagKey        string       `json:"flag_key"`
	VariationIndex *int         `json:"variation_index"`
	Value          any          `json:"value"`
	Reason         model.Reason `json:"reason"`
}

func (s *Server) sdkEvaluate(w http.ResponseWriter, r *http.Request) {
	rec := s.authenticateSDK(w, r)
	if rec == nil {
		return
	}
	var req evaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	snap, err := s.builder.Build(rec.ProjectKey, rec.EnvKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build snapshot")
		return
	}
	rf, ok := snap.Flags[req.FlagKey]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("flag %q not found", req.FlagKey))
		return
	}
	flag := &model.Flag{
		Key:        rf.Key,
		Name:       rf.Name,
		Type:       rf.Type,
		Variations: rf.Variations,
		Tags:       rf.Tags,
		Temporary:  rf.Temporary,
		Archived:   rf.Archived,
	}
	idx, val, reason := s.evalFlag(snap, flag, rf.Config, &req.Context)
	metrics.FlagEvaluations.With(map[string]string{
		"flag_key": req.FlagKey,
		"reason":   string(reason.Kind),
	}).Inc()
	writeJSON(w, http.StatusOK, evaluateResponse{
		FlagKey:        req.FlagKey,
		VariationIndex: idx,
		Value:          val,
		Reason:         reason,
	})
}

func (s *Server) sdkTrack(w http.ResponseWriter, r *http.Request) {
	rec := s.authenticateSDK(w, r)
	if rec == nil {
		return
	}
	if s.ingestor == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var events []analytics.Event
	if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	for i := range events {
		events[i].ProjectKey = rec.ProjectKey
		events[i].EnvironmentKey = rec.EnvKey
		s.ingestor.Track(&events[i])
		metrics.EventsIngested.With(map[string]string{"kind": string(events[i].Kind)}).Inc()
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Helpers ----

func (s *Server) authenticateSDK(w http.ResponseWriter, r *http.Request) *sdkauth.SDKKeyRecord {
	keyValue := sdkauth.ExtractBearer(r.Header.Get("Authorization"))
	if keyValue == "" {
		writeError(w, http.StatusUnauthorized, "missing SDK key")
		return nil
	}
	rec := s.auth.Lookup(keyValue)
	if rec == nil {
		writeError(w, http.StatusUnauthorized, "invalid or revoked SDK key")
		return nil
	}
	return rec
}

func (s *Server) appendAudit(r *http.Request, action, resource, resourceID string, before, after any) {
	var beforeBytes, afterBytes []byte
	if before != nil {
		beforeBytes, _ = json.Marshal(before)
	}
	if after != nil {
		afterBytes, _ = json.Marshal(after)
	}
	actor := "anonymous"
	if claims := auth.GetClaims(r); claims != nil {
		actor = claims.Email
	}
	_ = s.store.AppendAudit(&store.AuditEntry{
		Actor:      actor,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Before:     beforeBytes,
		After:      afterBytes,
		At:         time.Now().UnixMilli(),
	})
}

func (s *Server) publishFlagChange(r *http.Request, projectKey string, flag *model.Flag) {
	// For each environment, bump version and send SSE update
	envs, err := s.store.ListEnvironments(projectKey)
	if err != nil {
		return
	}
	for _, env := range envs {
		newVersion, err := s.store.IncrementEnvVersion(projectKey, env.Key)
		if err != nil {
			continue
		}
		snap, err := s.builder.Build(projectKey, env.Key)
		if err != nil {
			continue
		}
		s.hub.Publish(env.Key, stream.Message{
			Event:   "put",
			Version: newVersion,
			Data:    snap,
		})
	}
}

// snapshotStore wraps a Snapshot to satisfy eval.Store.
type snapshotStore struct {
	snap *snapshot.Snapshot
}

func (ss *snapshotStore) GetFlag(key string) (*model.Flag, *model.FlagConfig, bool) {
	rf, ok := ss.snap.Flags[key]
	if !ok {
		return nil, nil, false
	}
	flag := &model.Flag{
		Key:        rf.Key,
		Name:       rf.Name,
		Type:       rf.Type,
		Variations: rf.Variations,
		Tags:       rf.Tags,
		Temporary:  rf.Temporary,
		Archived:   rf.Archived,
	}
	return flag, rf.Config, true
}

func (ss *snapshotStore) GetSegment(key string) (*model.Segment, bool) {
	seg, ok := ss.snap.Segments[key]
	return seg, ok
}

func (s *Server) evalFlag(snap *snapshot.Snapshot, flag *model.Flag, cfg *model.FlagConfig, ctx *model.Context) (*int, any, model.Reason) {
	ss := &snapshotStore{snap: snap}
	return eval.Evaluate(flag, cfg, ctx, ss)
}
