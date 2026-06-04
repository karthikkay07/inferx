package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/inferbolthq/inferbolt/internal/drift"
	"github.com/inferbolthq/inferbolt/internal/metrics"
)

func main() {
	// Configure structured logging
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// Read environment variables
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8083"
	}

	slackWebhookURL := os.Getenv("SLACK_WEBHOOK_URL")
	
	checkInterval := parseDuration("DRIFT_CHECK_INTERVAL", 5*time.Minute)
	lookbackWindow := parseDuration("DRIFT_LOOKBACK", 1*time.Hour)
	warningPct := parseFloat("DRIFT_WARNING_PCT", 15.0)
	criticalPct := parseFloat("DRIFT_CRITICAL_PCT", 30.0)

	// Create database connection pool
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		slog.Error("failed to create database pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Create services
	metricsWriter := metrics.NewMetricsWriter(pool)
	baselineStore := drift.NewBaselineStore(pool)
	
	detectorConfig := drift.DetectorConfig{
		WarningThresholdPct:  warningPct,
		CriticalThresholdPct: criticalPct,
		CheckInterval:        checkInterval,
		LookbackWindow:       lookbackWindow,
		MinSamples:           10,
	}

	detector := drift.NewDetector(detectorConfig, metricsWriter, baselineStore, pool)
	notifier := drift.NewNotifier(slackWebhookURL)

	// Start background services
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	detector.Start(ctx)
	notifier.Start(ctx, detector.Alerts())

	// Create router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Routes
	r.Get("/v1/metrics", metricsHandler(metricsWriter))
	r.Get("/v1/baselines", baselinesHandler(baselineStore))
	r.Post("/v1/baselines/reset", resetBaselineHandler(pool))
	r.Get("/health", healthHandler)

	// Start server
	server := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	slog.Info("starting collector server", 
		"port", port,
		"check_interval", checkInterval,
		"lookback_window", lookbackWindow,
		"warning_threshold_pct", warningPct,
		"critical_threshold_pct", criticalPct,
		"slack_enabled", slackWebhookURL != "")

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	slog.Info("shutting down server")
	cancel() // Stop detector and notifier

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
}

func parseDuration(envVar string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(envVar); val != "" {
		if parsed, err := time.ParseDuration(val); err == nil {
			return parsed
		}
		slog.Warn("invalid duration, using default", "env_var", envVar, "value", val, "default", defaultVal)
	}
	return defaultVal
}

func parseFloat(envVar string, defaultVal float64) float64 {
	if val := os.Getenv(envVar); val != "" {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil {
			return parsed
		}
		slog.Warn("invalid float, using default", "env_var", envVar, "value", val, "default", defaultVal)
	}
	return defaultVal
}

func metricsHandler(metricsWriter *metrics.MetricsWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		engine := r.URL.Query().Get("engine")
		model := r.URL.Query().Get("model")
		sinceStr := r.URL.Query().Get("since")

		if engine == "" || model == "" {
			http.Error(w, "engine and model parameters are required", http.StatusBadRequest)
			return
		}

		var since time.Time
		var err error
		if sinceStr != "" {
			since, err = time.Parse(time.RFC3339, sinceStr)
			if err != nil {
				http.Error(w, "invalid since format (use RFC3339)", http.StatusBadRequest)
				return
			}
		} else {
			since = time.Now().Add(-24 * time.Hour) // Default to last 24 hours
		}

		results, err := metricsWriter.QueryByEngineAndModel(r.Context(), engine, model, since)
		if err != nil {
			slog.Error("failed to query metrics", "error", err)
			http.Error(w, "failed to query metrics", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

func baselinesHandler(baselineStore *drift.BaselineStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		engine := r.URL.Query().Get("engine")
		model := r.URL.Query().Get("model")

		if engine == "" || model == "" {
			http.Error(w, "engine and model parameters are required", http.StatusBadRequest)
			return
		}

		baseline, err := baselineStore.Get(r.Context(), engine, model)
		if err != nil {
			slog.Error("failed to get baseline", "error", err)
			http.Error(w, "failed to get baseline", http.StatusInternalServerError)
			return
		}

		if baseline == nil {
			http.Error(w, "baseline not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(baseline)
	}
}

type ResetBaselineRequest struct {
	Engine string `json:"engine"`
	Model  string `json:"model"`
}

func resetBaselineHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ResetBaselineRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if req.Engine == "" || req.Model == "" {
			http.Error(w, "engine and model are required", http.StatusBadRequest)
			return
		}

		// Delete baseline from database
		_, err := pool.Exec(r.Context(), 
			"DELETE FROM public.baselines WHERE engine = $1 AND model = $2", 
			req.Engine, req.Model)
		if err != nil {
			slog.Error("failed to delete baseline", "error", err)
			http.Error(w, "failed to delete baseline", http.StatusInternalServerError)
			return
		}

		slog.Info("baseline reset", "engine", req.Engine, "model", req.Model)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "reset"})
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{"status": "ok"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}