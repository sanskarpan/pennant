package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"pennant/internal/analytics"
	"pennant/internal/api"
	"pennant/internal/auth"
	"pennant/internal/events"
	"pennant/internal/experiment"
	"pennant/internal/model"
	"pennant/internal/sdkauth"
	"pennant/internal/snapshot"
	"pennant/internal/store"
	"pennant/internal/stream"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Infrastructure
	bus := events.NewBus()
	ms, err := store.NewStore()
	if err != nil {
		slog.Error("failed to open store", "err", err)
		os.Exit(1)
	}
	builder := snapshot.NewBuilder(ms)
	hub := stream.NewHub(bus)
	sdkAuthenticator := sdkauth.NewAuthenticator()

	// Analytics pipeline
	eventStore := analytics.NewMemEventStore()
	ingestor := analytics.NewIngestor(eventStore, 5*time.Second, 500)
	ingestor.Start()
	defer ingestor.Stop()

	// Seed a default project + environment for demonstration
	if err := seedDemoData(ms, sdkAuthenticator); err != nil {
		slog.Error("failed to seed demo data", "err", err)
	}

	adminToken := os.Getenv("PENNANT_ADMIN_TOKEN") // kept for backward compat

	// JWT + RBAC auth
	jwtSecret := os.Getenv("PENNANT_JWT_SECRET")
	var userStore auth.UserStorer
	if sqlitePath := os.Getenv("SQLITE_PATH"); sqlitePath != "" {
		us, err := auth.NewSqliteUserStore(sqlitePath + ".users")
		if err != nil {
			slog.Warn("failed to open persistent user store, falling back to in-memory", "err", err)
			userStore = auth.NewUserStore()
		} else {
			userStore = us
		}
	} else {
		userStore = auth.NewUserStore()
	}
	jwtService := auth.NewJWTService(jwtSecret)

	// Seed initial admin user
	if err := seedAdminUser(userStore); err != nil {
		slog.Warn("admin user seed skipped", "err", err)
	}

	// CORS origins
	var corsOrigins []string
	if raw := os.Getenv("PENNANT_CORS_ORIGINS"); raw != "" {
		corsOrigins = strings.Split(raw, ",")
	}

	// Experiment engine
	expStore := experiment.NewMemExperimentStore()
	eng := experiment.NewEngine(eventStore, expStore)

	// HTTP server
	srv := api.NewServer(ms, builder, hub, sdkAuthenticator, ingestor, adminToken, userStore, jwtService, corsOrigins, expStore, eng)
	httpSrv := &http.Server{
		Addr:         *addr,
		Handler:      srv,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE streams can be long-lived
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	certFile := os.Getenv("TLS_CERT_FILE")
	keyFile := os.Getenv("TLS_KEY_FILE")
	autoDomain := os.Getenv("TLS_AUTO_DOMAIN")

	go func() {
		slog.Info("server listening", "addr", *addr)
		var serveErr error
		switch {
		case autoDomain != "":
			m := &autocert.Manager{
				Cache:      autocert.DirCache("./tls-cache"),
				Prompt:     autocert.AcceptTOS,
				HostPolicy: autocert.HostWhitelist(autoDomain),
			}
			// HTTP redirect server on :80
			go func() {
				if err := http.ListenAndServe(":80", m.HTTPHandler(nil)); err != nil && err != http.ErrServerClosed {
					slog.Error("HTTP redirect server error", "err", err)
				}
			}()
			httpSrv.Addr = ":443"
			httpSrv.TLSConfig = m.TLSConfig()
			serveErr = httpSrv.ListenAndServeTLS("", "")
		case certFile != "" && keyFile != "":
			// HTTP → HTTPS redirect on :80
			go func() {
				redirectSrv := &http.Server{
					Addr: ":80",
					Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						target := "https://" + r.Host + r.URL.RequestURI()
						http.Redirect(w, r, target, http.StatusMovedPermanently)
					}),
					ReadTimeout: 5 * time.Second,
					IdleTimeout: 60 * time.Second,
				}
				if err := redirectSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					slog.Error("HTTP redirect server error", "err", err)
				}
			}()
			serveErr = httpSrv.ListenAndServeTLS(certFile, keyFile)
		default:
			serveErr = httpSrv.ListenAndServe()
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			slog.Error("server error", "err", serveErr)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
	slog.Info("server stopped")
}

func seedAdminUser(us auth.UserStorer) error {
	hash, err := auth.HashPassword("admin")
	if err != nil {
		return err
	}
	return us.CreateUser(&auth.User{
		ID:           "admin-0",
		Email:        "admin@pennant.local",
		Name:         "Admin",
		Role:         auth.RoleOwner,
		PasswordHash: hash,
	})
}

func seedDemoData(ms store.ConfigStore, sdkAuth *sdkauth.Authenticator) error {
	proj := &model.Project{Key: "default", Name: "Default Project"}
	if err := ms.CreateProject(proj); err != nil {
		return err
	}
	env := &model.Environment{
		Key:  "production",
		Name: "Production",
		SDKKeys: []model.SDKKey{
			{Value: "sdk-server-default-prod", Type: model.SDKKeyServer},
			{Value: "sdk-client-default-prod", Type: model.SDKKeyClient},
		},
	}
	if err := ms.CreateEnvironment("default", env); err != nil {
		return err
	}
	// Register SDK keys with the authenticator
	sdkAuth.Register(&sdkauth.SDKKeyRecord{
		Value:      "sdk-server-default-prod",
		ProjectKey: "default",
		EnvKey:     "production",
		Type:       "server",
	})
	sdkAuth.Register(&sdkauth.SDKKeyRecord{
		Value:      "sdk-client-default-prod",
		ProjectKey: "default",
		EnvKey:     "production",
		Type:       "client",
	})
	fmt.Println("Demo data seeded: project=default env=production")
	return nil
}
