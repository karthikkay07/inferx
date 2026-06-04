package gateway

import (
	"context"
	"time"

	"github.com/inferbolthq/inferbolt/internal/jobs"
	"github.com/inferbolthq/inferbolt/internal/queue"
)

// JobStorer abstracts persistent job storage; satisfied by *config.Store.
type JobStorer interface {
	SaveJob(ctx context.Context, job jobs.Job) error
	GetJob(ctx context.Context, jobID, tenantID string) (*jobs.Job, error)
	ListJobs(ctx context.Context, tenantID, state string, limit, offset int) ([]jobs.Job, error)
	CountJobs(ctx context.Context, tenantID, state string) (int, error)
	UpdateJobState(ctx context.Context, jobID string, state jobs.JobState, errMsg string) error
}

// JobQueuer abstracts job enqueueing; satisfied by *queue.QueueClient.
type JobQueuer interface {
	Enqueue(ctx context.Context, args queue.BenchmarkJobArgs) error
}

// MetricsReader abstracts benchmark result queries; satisfied by *metrics.MetricsWriter.
type MetricsReader interface {
	QueryByJob(ctx context.Context, jobID string) ([]jobs.Result, error)
	QueryByEngineAndModel(ctx context.Context, engine, model string, since time.Time) ([]jobs.Result, error)
}

// DBPinger abstracts a database ping; satisfied by *pgxpool.Pool.
type DBPinger interface {
	Ping(ctx context.Context) error
}
