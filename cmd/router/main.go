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

	"github.com/dgraph-io/ristretto"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/inferbolthq/inferbolt/internal/router"
	"github.com/inferbolthq/inferbolt/internal/workers"
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

	orchestratorURL := os.Getenv("ORCHESTRATOR_URL")
	if orchestratorURL == "" {
		slog.Error("ORCHESTRATOR_URL is required")
		os.Exit(1)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	// Create database connection pool
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		slog.Error("failed to create database pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Create ristretto cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,
		MaxCost:     50 << 20, // 50MB
		BufferItems: 64,
	})
	if err != nil {
		slog.Error("failed to create cache", "error", err)
		os.Exit(1)
	}
	defer cache.Close()

	// Create engine selector
	selector := router.NewEngineSelector(cache)

	// Create router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Routes
	r.Post("/v1/route", routeHandler(selector, orchestratorURL))
	r.Get("/health", healthHandler)

	// Start server
	server := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	slog.Info("starting router server", "port", port)

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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
}

type RouteRequest struct {
	router.ClassificationInput
	GPUProfile string `json:"gpu_profile"`
	TenantID   string `json:"tenant_id"`
}

func routeHandler(selector *router.EngineSelector, orchestratorURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RouteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			slog.Error("invalid request body", "error", err)
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Classify workload
		classification := router.Classify(req.ClassificationInput)

		// Fetch current worker status from orchestrator
		workers, err := fetchWorkerStatus(r.Context(), orchestratorURL)
		if err != nil {
			slog.Error("failed to fetch worker status", "error", err)
			http.Error(w, "failed to fetch worker status", http.StatusInternalServerError)
			return
		}

		// Select worker
		selectionReq := router.SelectionRequest{
			Classification:  classification,
			GPUProfile:      req.GPUProfile,
			TenantID:        req.TenantID,
			FallbackAllowed: true,
		}

		result, err := selector.Select(r.Context(), selectionReq, workers)
		if err != nil {
			slog.Error("worker selection failed", "error", err)
			if err == router.ErrNoWorkerAvailable {
				http.Error(w, "no worker available", http.StatusServiceUnavailable)
			} else {
				http.Error(w, "worker selection failed", http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func fetchWorkerStatus(ctx context.Context, orchestratorURL string) ([]router.ExtendedWorkerEntry, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	
	req, err := http.NewRequestWithContext(ctx, "GET", orchestratorURL+"/internal/workers", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching workers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("orchestrator returned status %d", resp.StatusCode)
	}

	var rawWorkers []workers.WorkerEntry
	if err := json.NewDecoder(resp.Body).Decode(&rawWorkers); err != nil {
		return nil, fmt.Errorf("decoding workers: %w", err)
	}

	// Convert to extended worker entries with engine information
	// For now, we'll infer engine from worker ID or URL
	// In a real implementation, this would come from the orchestrator
	extendedWorkers := make([]router.ExtendedWorkerEntry, len(rawWorkers))
	for i, worker := range rawWorkers {
		extendedWorkers[i] = router.ExtendedWorkerEntry{
			WorkerEntry: worker,
			Engine:      inferEngineFromWorker(worker),
		}
	}

	return extendedWorkers, nil
}

// inferEngineFromWorker infers the engine type from worker metadata
// This is a placeholder - in production this would come from worker registration
func inferEngineFromWorker(worker workers.WorkerEntry) string {
	// Simple heuristic based on worker ID pattern
	// In production, workers would register with explicit engine type
	if len(worker.ID) > 0 {
		switch worker.ID[0] {
		case 'v':
			return "vllm"
		case 's':
			return "sglang"
		case 't':
			return "tensorrt"
		case 'l':
			return "llamacpp"
		}
	}
	return "vllm" // default
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{"status": "ok"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}