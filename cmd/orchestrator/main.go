package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	cfg "github.com/inferbolthq/inferbolt/internal/config"
	"github.com/inferbolthq/inferbolt/internal/jobs"
	"github.com/inferbolthq/inferbolt/internal/metrics"
	"github.com/inferbolthq/inferbolt/internal/queue"
	"github.com/inferbolthq/inferbolt/internal/workers"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	port := getenv("PORT", "8081")

	evictionInterval, err := time.ParseDuration(getenv("WORKER_EVICTION_INTERVAL", "10s"))
	if err != nil {
		slog.Error("invalid WORKER_EVICTION_INTERVAL", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ── Database pool ──────────────────────────────────────────────────────
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("connect to database", "error", err)
		os.Exit(1)
	}

	// ── Dependencies ───────────────────────────────────────────────────────
	queueClient, err := queue.NewQueueClient(ctx, pool)
	if err != nil {
		slog.Error("init queue client", "error", err)
		os.Exit(1)
	}

	registry := workers.NewWorkerRegistry()
	metricsWriter := metrics.NewMetricsWriter(pool)
	store := cfg.NewStore(pool)

	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,
		MaxCost:     100 * 1024 * 1024, // 100 MB
		BufferItems: 64,
	})
	if err != nil {
		slog.Error("init cache", "error", err)
		os.Exit(1)
	}

	// ── River worker registration ──────────────────────────────────────────
	riverWorkers := river.NewWorkers()
	benchmarkWorker := jobs.NewBenchmarkWorker(pool, registry, cache)
	river.AddWorker(riverWorkers, benchmarkWorker)

	// ── Background services ────────────────────────────────────────────────
	registry.StartHeartbeatEviction(ctx, evictionInterval)

	go func() {
		if err := queueClient.Start(ctx, riverWorkers); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("river client error", "error", err)
		}
	}()

	// ── HTTP server (internal routes, no auth) ─────────────────────────────
	r := chi.NewRouter()

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	})

	r.Post("/internal/workers/register", func(w http.ResponseWriter, r *http.Request) {
		var entry workers.WorkerEntry
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		registry.Register(entry)
		slog.Info("worker registered", "id", entry.ID, "gpu_type", entry.GPUType)
		w.WriteHeader(http.StatusOK)
	})

	r.Post("/internal/workers/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			WorkerID string `json:"worker_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		registry.Heartbeat(req.WorkerID)
		w.WriteHeader(http.StatusOK)
	})

	r.Post("/internal/results", func(w http.ResponseWriter, r *http.Request) {
		var result jobs.Result
		if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}

		if err := metricsWriter.WriteBenchResultBatch(r.Context(), []jobs.Result{result}); err != nil {
			slog.Error("write bench result", "error", err)
			http.Error(w, `{"error":"failed to write metrics"}`, http.StatusInternalServerError)
			return
		}

		if err := store.UpdateJobState(r.Context(), result.JobID, jobs.StateCompleted, ""); err != nil {
			slog.Error("update job state", "error", err)
		}

		rec := jobs.Recommendation{
			JobID:       result.JobID,
			BestEngine:  result.Engine,
			BestConfig:  result.Config,
			CostPerMTok: result.CostPerMTok,
			TokPerSec:   result.TokPerSec,
			Reasoning:   "selected based on highest throughput from submitted result",
		}
		if err := store.SaveRecommendation(r.Context(), rec); err != nil {
			slog.Error("save recommendation", "error", err)
		}

		if err := store.AppendAuditLog(r.Context(), "internal", "job.completed", result.JobID); err != nil {
			slog.Error("append audit log", "error", err)
		}

		w.WriteHeader(http.StatusOK)
	})

	r.Get("/internal/jobs/{jobID}/state", func(w http.ResponseWriter, r *http.Request) {
		jobID := chi.URLParam(r, "jobID")
		job, err := store.GetJob(r.Context(), jobID, "internal")
		if err != nil {
			http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"state": string(job.State)}) //nolint:errcheck
	})

	httpServer := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		slog.Info("orchestrator HTTP listening", "port", port)
		if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server error", "error", err)
		}
	}()

	// ── Graceful shutdown ──────────────────────────────────────────────────
	<-ctx.Done()
	slog.Info("shutting down orchestrator")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown error", "error", err)
	}
	pool.Close()
	slog.Info("orchestrator stopped")
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
