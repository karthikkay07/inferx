package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/inferbolthq/inferbolt/internal/queue"
	"github.com/inferbolthq/inferbolt/internal/workers"
)

type BenchmarkWorker struct {
	river.WorkerDefaults[queue.BenchmarkJobArgs]
	db         *pgxpool.Pool
	registry   *workers.WorkerRegistry
	cache      *ristretto.Cache
	httpClient *http.Client
}

func NewBenchmarkWorker(db *pgxpool.Pool, reg *workers.WorkerRegistry, cache *ristretto.Cache) *BenchmarkWorker {
	return &BenchmarkWorker{
		db:         db,
		registry:   reg,
		cache:      cache,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (w *BenchmarkWorker) Work(ctx context.Context, job *river.Job[queue.BenchmarkJobArgs]) error {
	// Step 1: transition to Running — River retries if this fails.
	if err := updateJobState(ctx, w.db, job.Args.JobID, StateRunning); err != nil {
		return fmt.Errorf("transition to running: %w", err)
	}

	// Step 2: find an idle Python worker for the requested GPU profile.
	entry, err := w.registry.SelectWorker(job.Args.GPUProfile)
	if err != nil {
		if errors.Is(err, workers.ErrNoAvailableWorker) {
			return fmt.Errorf("no available worker for gpu_profile=%s: %w", job.Args.GPUProfile, err)
		}
		return fmt.Errorf("select worker: %w", err)
	}

	// Step 3: mark worker busy; always release on return.
	w.registry.MarkBusy(entry.ID, job.Args.JobID)
	defer w.registry.MarkIdle(entry.ID)

	// Step 4: POST job args to the Python worker.
	body, err := json.Marshal(job.Args)
	if err != nil {
		return fmt.Errorf("marshal job args: %w", err)
	}
	dispatchCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(dispatchCtx, http.MethodPost, entry.URL+"/run", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build dispatch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		_ = updateJobState(ctx, w.db, job.Args.JobID, StateFailed)
		return fmt.Errorf("dispatch to worker: %w", err)
	}
	defer resp.Body.Close()

	var runResp struct {
		RunID string `json:"run_id"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&runResp); err != nil {
		_ = updateJobState(ctx, w.db, job.Args.JobID, StateFailed)
		return fmt.Errorf("decode run response: %w", err)
	}

	// Step 5: transition to Collecting now that the run is started.
	if err = updateJobState(ctx, w.db, job.Args.JobID, StateCollecting); err != nil {
		return fmt.Errorf("transition to collecting: %w", err)
	}

	// Step 6: poll the Python worker until the run finishes.
	return w.pollForResults(ctx, job.Args.JobID, runResp.RunID, entry.URL)
}

// updateJobState reads the current state, validates the transition, and persists the new state.
func updateJobState(ctx context.Context, db *pgxpool.Pool, jobID string, newState JobState) error {
	var current JobState
	if err := db.QueryRow(ctx,
		"SELECT state FROM public.jobs WHERE id = $1", jobID,
	).Scan(&current); err != nil {
		return fmt.Errorf("read current state: %w", err)
	}
	if !ValidTransition(current, newState) {
		return fmt.Errorf("invalid state transition: %s → %s", current, newState)
	}
	_, err := db.Exec(ctx,
		"UPDATE public.jobs SET state = $1, updated_at = NOW() WHERE id = $2",
		string(newState), jobID,
	)
	return err
}

// pollForResults polls the Python worker every 5 s, timing out after 45 min.
func (w *BenchmarkWorker) pollForResults(ctx context.Context, jobID, runID, workerURL string) error {
	timeout := time.NewTimer(45 * time.Minute)
	defer timeout.Stop()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("poll timeout (45m) for job=%s run=%s", jobID, runID)
		case <-ticker.C:
			status, msg, err := w.fetchRunStatus(runID, workerURL)
			if err != nil {
				continue // transient network error — retry on next tick
			}
			switch status {
			case "completed":
				return nil
			case "failed":
				return fmt.Errorf("worker reported failure for run=%s: %s", runID, msg)
			}
		}
	}
}

func (w *BenchmarkWorker) fetchRunStatus(runID, workerURL string) (status, message string, err error) {
	reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet,
		workerURL+"/run/"+runID+"/status", nil)
	if err != nil {
		return "", "", err
	}
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var body struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", "", err
	}
	return body.Status, body.Message, nil
}
