package router

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/inferbolthq/inferbolt/internal/workers"
)

var ErrNoWorkerAvailable = errors.New("no worker available")

type EngineSelector struct {
	cache *ristretto.Cache
}

func NewEngineSelector(cache *ristretto.Cache) *EngineSelector {
	return &EngineSelector{
		cache: cache,
	}
}

type SelectionRequest struct {
	Classification   ClassificationResult `json:"classification"`
	GPUProfile       string               `json:"gpu_profile"`
	TenantID         string               `json:"tenant_id"`
	FallbackAllowed  bool                 `json:"fallback_allowed"`
}

type SelectionResult struct {
	Engine         string `json:"engine"`
	WorkerID       string `json:"worker_id"`
	WorkerURL      string `json:"worker_url"`
	UsedFallback   bool   `json:"used_fallback"`
	FallbackReason string `json:"fallback_reason,omitempty"`
}

func (s *EngineSelector) Select(ctx context.Context, req SelectionRequest, availableWorkers []ExtendedWorkerEntry) (SelectionResult, error) {
	cacheKey := fmt.Sprintf("sel:%s:%s", req.TenantID, req.GPUProfile)
	
	// Check cache first
	if cached, found := s.cache.Get(cacheKey); found {
		if result, ok := cached.(SelectionResult); ok {
			slog.Info("using cached selection", 
				"tenant_id", req.TenantID,
				"gpu_profile", req.GPUProfile,
				"engine", result.Engine,
				"worker_id", result.WorkerID)
			return result, nil
		}
	}

	// First attempt: find worker matching recommended engine and GPU profile
	for _, worker := range availableWorkers {
		if worker.Status == workers.StatusIdle && 
		   worker.Engine == req.Classification.RecommendedEngine && 
		   worker.GPUType == req.GPUProfile {
			
			result := SelectionResult{
				Engine:       worker.Engine,
				WorkerID:     worker.ID,
				WorkerURL:    worker.URL,
				UsedFallback: false,
			}

			// Cache the result
			s.cache.SetWithTTL(cacheKey, result, 1, 30*time.Second)

			slog.Info("selected optimal worker",
				"tenant_id", req.TenantID,
				"engine", result.Engine,
				"worker_id", result.WorkerID,
				"fallback_used", false)

			return result, nil
		}
	}

	// Fallback attempt: find any idle worker with matching GPU profile
	if req.FallbackAllowed {
		for _, worker := range availableWorkers {
			if worker.Status == workers.StatusIdle && worker.GPUType == req.GPUProfile {
				result := SelectionResult{
					Engine:         worker.Engine,
					WorkerID:       worker.ID,
					WorkerURL:      worker.URL,
					UsedFallback:   true,
					FallbackReason: fmt.Sprintf("no idle %s workers available, using %s instead", req.Classification.RecommendedEngine, worker.Engine),
				}

				// Cache the fallback result
				s.cache.SetWithTTL(cacheKey, result, 1, 30*time.Second)

				slog.Info("selected fallback worker",
					"tenant_id", req.TenantID,
					"recommended_engine", req.Classification.RecommendedEngine,
					"selected_engine", result.Engine,
					"worker_id", result.WorkerID,
					"fallback_reason", result.FallbackReason)

				return result, nil
			}
		}
	}

	slog.Warn("no workers available",
		"tenant_id", req.TenantID,
		"gpu_profile", req.GPUProfile,
		"recommended_engine", req.Classification.RecommendedEngine,
		"fallback_allowed", req.FallbackAllowed)

	return SelectionResult{}, ErrNoWorkerAvailable
}

// Extended worker entry with engine information
type ExtendedWorkerEntry struct {
	workers.WorkerEntry
	Engine string `json:"engine"`
}

