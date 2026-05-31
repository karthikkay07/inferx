package workers

import (
	"context"
	"errors"
	"sync"
	"time"
)

type WorkerStatus string

const (
	StatusIdle    WorkerStatus = "idle"
	StatusBusy    WorkerStatus = "busy"
	StatusOffline WorkerStatus = "offline"
)

// ErrNoAvailableWorker is returned by SelectWorker when no idle matching worker exists.
var ErrNoAvailableWorker = errors.New("no available worker")

type WorkerEntry struct {
	ID           string       `json:"id"`
	URL          string       `json:"url"`
	GPUType      string       `json:"gpu_type"`
	Status       WorkerStatus `json:"status"`
	LastSeen     time.Time    `json:"last_seen"`
	CurrentJobID string       `json:"current_job_id,omitempty"`
}

type WorkerRegistry struct {
	mu      sync.RWMutex
	workers map[string]*WorkerEntry
}

func NewWorkerRegistry() *WorkerRegistry {
	return &WorkerRegistry{workers: make(map[string]*WorkerEntry)}
}

func (r *WorkerRegistry) Register(entry WorkerEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.LastSeen = time.Now()
	entry.Status = StatusIdle
	r.workers[entry.ID] = &entry
}

func (r *WorkerRegistry) Heartbeat(workerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.workers[workerID]
	if !ok {
		return
	}
	w.LastSeen = time.Now()
	if w.Status == StatusOffline {
		w.Status = StatusIdle
	}
}

func (r *WorkerRegistry) MarkBusy(workerID, jobID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.workers[workerID]
	if !ok {
		return
	}
	w.Status = StatusBusy
	w.CurrentJobID = jobID
}

func (r *WorkerRegistry) MarkIdle(workerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.workers[workerID]
	if !ok {
		return
	}
	w.Status = StatusIdle
	w.CurrentJobID = ""
}

// SelectWorker returns the first idle worker matching gpuProfile.
func (r *WorkerRegistry) SelectWorker(gpuProfile string) (*WorkerEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, w := range r.workers {
		if w.Status == StatusIdle && w.GPUType == gpuProfile {
			return w, nil
		}
	}
	return nil, ErrNoAvailableWorker
}

// EvictStale marks workers that haven't sent a heartbeat within timeout as offline.
func (r *WorkerRegistry) EvictStale(timeout time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := time.Now().Add(-timeout)
	for _, w := range r.workers {
		if w.LastSeen.Before(cutoff) {
			w.Status = StatusOffline
		}
	}
}

func (r *WorkerRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.workers)
}

// StartHeartbeatEviction runs EvictStale(30s) on every tick until ctx is cancelled.
func (r *WorkerRegistry) StartHeartbeatEviction(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.EvictStale(30 * time.Second)
			}
		}
	}()
}
