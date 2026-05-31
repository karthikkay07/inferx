package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// WorkloadConfig mirrors jobs.WorkloadConfig for River queue serialization.
// Defined here to avoid an import cycle between the jobs and queue packages.
type WorkloadConfig struct {
	Concurrency  int `json:"concurrency"`
	PromptTokens int `json:"prompt_tokens"`
	OutputTokens int `json:"output_tokens"`
	NumRequests  int `json:"num_requests"`
}

type BenchmarkJobArgs struct {
	JobID      string         `json:"job_id"`
	Model      string         `json:"model"`
	Engines    []string       `json:"engines"`
	Workload   WorkloadConfig `json:"workload"`
	GPUProfile string         `json:"gpu_profile"`
	TenantID   string         `json:"tenant_id"`
}

func (BenchmarkJobArgs) Kind() string { return "benchmark_job" }

type QueueClient struct {
	client *river.Client[pgx.Tx]
	pool   *pgxpool.Pool
}

// NewQueueClient runs River schema migrations and returns an insert-ready client.
// Workers are registered later via Start.
func NewQueueClient(ctx context.Context, pool *pgxpool.Pool) (*QueueClient, error) {
	driver := riverpgxv5.New(pool)

	migrator, err := rivermigrate.New(driver, nil)
	if err != nil {
		return nil, fmt.Errorf("new river migrator: %w", err)
	}
	if _, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return nil, fmt.Errorf("river migrate up: %w", err)
	}

	// Insert-only client — no workers configured until Start is called.
	rc, err := river.NewClient[pgx.Tx](driver, &river.Config{})
	if err != nil {
		return nil, fmt.Errorf("new river client: %w", err)
	}

	return &QueueClient{client: rc, pool: pool}, nil
}

func (q *QueueClient) Enqueue(ctx context.Context, args BenchmarkJobArgs) (*river.InsertResult, error) {
	res, err := q.client.Insert(ctx, args, &river.InsertOpts{
		MaxAttempts: 3,
		Queue:       "benchmarks",
	})
	if err != nil {
		return nil, fmt.Errorf("enqueue benchmark job: %w", err)
	}
	return res, nil
}

// Start creates a worker-enabled River client and begins processing jobs.
// It blocks until ctx is cancelled.
func (q *QueueClient) Start(ctx context.Context, workers *river.Workers) error {
	rc, err := river.NewClient[pgx.Tx](riverpgxv5.New(q.pool), &river.Config{
		Queues:  map[string]river.QueueConfig{"benchmarks": {MaxWorkers: 10}},
		Workers: workers,
	})
	if err != nil {
		return fmt.Errorf("new river worker client: %w", err)
	}
	q.client = rc
	if err = rc.Start(ctx); err != nil {
		return fmt.Errorf("river start: %w", err)
	}
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return rc.Stop(stopCtx)
}
