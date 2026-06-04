package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"

	iauth "github.com/inferbolthq/inferbolt/internal/auth"
	"github.com/inferbolthq/inferbolt/internal/config"
	"github.com/inferbolthq/inferbolt/internal/gateway"
	"github.com/inferbolthq/inferbolt/internal/metrics"
	"github.com/inferbolthq/inferbolt/internal/queue"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	databaseURL := mustEnv("DATABASE_URL")
	jwtSecret := mustEnv("JWT_SECRET")
	orchestratorURL := mustEnv("ORCHESTRATOR_URL")

	if len(jwtSecret) < 32 {
		slog.Error("JWT_SECRET must be at least 32 characters")
		os.Exit(1)
	}

	port := getenv("PORT", "8080")
	env := getenv("ENV", "development")
	otelEndpoint := getenv("OTEL_ENDPOINT", "")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		slog.Error("failed to create db pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,
		MaxCost:     200 << 20,
		BufferItems: 64,
	})
	if err != nil {
		slog.Error("failed to create cache", "error", err)
		os.Exit(1)
	}
	defer cache.Close()

	km := iauth.NewKeyManager(jwtSecret, cache)
	mw := metrics.NewMetricsWriter(pool)
	store := config.NewStore(pool)

	queueClient, err := queue.NewQueueClient(ctx, pool)
	if err != nil {
		slog.Error("failed to create queue client", "error", err)
		os.Exit(1)
	}

	if otelEndpoint != "" {
		slog.Info("OTel endpoint configured; set SDK env vars to enable exporter", "endpoint", otelEndpoint)
	}
	tracer := otel.Tracer("inferbolt/gateway")

	h := gateway.NewHandler(store, queueClient, mw, km, pool, pool, orchestratorURL)

	r := chi.NewRouter()
	r.Use(iauth.RequestIDMiddleware())
	r.Use(iauth.LoggerMiddleware())
	r.Use(iauth.OTelMiddleware(tracer))
	r.Use(iauth.TimeoutMiddleware(30 * time.Second))

	r.Get("/health", h.Health)

	r.Group(func(r chi.Router) {
		r.Use(iauth.AuthMiddleware(km))
		r.Use(iauth.RateLimitMiddleware(cache))

		r.Group(func(r chi.Router) {
			r.Use(iauth.RequireScope(km, iauth.ScopeJobsWrite))
			r.Post("/v1/jobs", h.CreateJob)
			r.Delete("/v1/jobs/{jobID}", h.CancelJob)
		})

		r.Group(func(r chi.Router) {
			r.Use(iauth.RequireScope(km, iauth.ScopeJobsRead))
			r.Get("/v1/jobs", h.ListJobs)
			r.Get("/v1/jobs/{jobID}", h.GetJob)
			r.Get("/v1/jobs/{jobID}/results", h.GetJobResults)
			r.Post("/v1/route", h.ClassifyWorkload)
			r.Get("/v1/engines", h.ListEngines)
		})

		r.Group(func(r chi.Router) {
			r.Use(iauth.RequireScope(km, iauth.ScopeMetricsRead))
			r.Get("/v1/metrics", h.GetMetrics)
		})

		r.Group(func(r chi.Router) {
			r.Use(iauth.RequireScope(km, iauth.ScopeAdminAll))
			r.Post("/v1/admin/apikeys", h.CreateAPIKey)
		})
	})

	if env == "development" {
		if err := bootstrapDevKey(ctx, pool, km); err != nil {
			slog.Warn("dev key bootstrap failed", "error", err)
		}
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("gateway listening", "port", port, "env", env)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	slog.Info("shutdown complete")
}

func bootstrapDevKey(ctx context.Context, pool *pgxpool.Pool, km *iauth.KeyManager) error {
	var count int
	if err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM public.api_keys WHERE tenant_id = 'dev'",
	).Scan(&count); err != nil {
		return fmt.Errorf("check existing dev keys: %w", err)
	}
	if count > 0 {
		return nil
	}

	scopes := []iauth.Scope{
		iauth.ScopeJobsWrite, iauth.ScopeJobsRead,
		iauth.ScopeMetricsRead, iauth.ScopeConfigWrite, iauth.ScopeAdminAll,
	}
	token, err := km.Issue("dev", scopes, 30*24*time.Hour)
	if err != nil {
		return fmt.Errorf("issue dev key: %w", err)
	}

	sum := sha256.Sum256([]byte(token))
	keyHash := hex.EncodeToString(sum[:])
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	_, err = pool.Exec(ctx,
		`INSERT INTO public.api_keys (tenant_id, key_hash, scopes, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		"dev", keyHash, []string{string(iauth.ScopeAdminAll)}, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("persist dev key: %w", err)
	}

	slog.Info("dev API key created", "token", token)
	return nil
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required env var not set", "var", key)
		os.Exit(1)
	}
	return v
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
