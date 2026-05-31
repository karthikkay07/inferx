package config

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/karthikkay07/inferx/internal/jobs"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) SaveJob(ctx context.Context, job jobs.Job) error {
	wcJSON, err := json.Marshal(job.WorkloadConfig)
	if err != nil {
		return fmt.Errorf("marshal workload config: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO public.jobs
		  (id, tenant_id, model, engines, workload_config, gpu_profile, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		job.ID, job.TenantID, job.Model, job.Engines,
		wcJSON, job.GPUProfile, string(job.State),
		job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("save job: %w", err)
	}
	return nil
}

func (s *Store) GetJob(ctx context.Context, jobID, tenantID string) (*jobs.Job, error) {
	var j jobs.Job
	var wcJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, model, engines, workload_config, gpu_profile,
		       state, created_at, updated_at,
		       COALESCE(error_msg, ''), COALESCE(run_id, '')
		FROM public.jobs
		WHERE id = $1 AND tenant_id = $2`,
		jobID, tenantID,
	).Scan(
		&j.ID, &j.TenantID, &j.Model, &j.Engines, &wcJSON, &j.GPUProfile,
		&j.State, &j.CreatedAt, &j.UpdatedAt, &j.ErrorMsg, &j.RunID,
	)
	if err != nil {
		return nil, fmt.Errorf("get job %s: %w", jobID, err)
	}
	if err = json.Unmarshal(wcJSON, &j.WorkloadConfig); err != nil {
		return nil, fmt.Errorf("unmarshal workload config: %w", err)
	}
	return &j, nil
}

func (s *Store) UpdateJobState(ctx context.Context, jobID string, state jobs.JobState, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE public.jobs
		SET state = $1, updated_at = NOW(), error_msg = NULLIF($2, '')
		WHERE id = $3`,
		string(state), errMsg, jobID,
	)
	if err != nil {
		return fmt.Errorf("update job state: %w", err)
	}
	return nil
}

func (s *Store) SetJobRunID(ctx context.Context, jobID, runID string) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE public.jobs SET run_id = $1, updated_at = NOW() WHERE id = $2",
		runID, jobID,
	)
	if err != nil {
		return fmt.Errorf("set job run_id: %w", err)
	}
	return nil
}

func (s *Store) SaveRecommendation(ctx context.Context, rec jobs.Recommendation) error {
	cfgJSON, err := json.Marshal(rec.BestConfig)
	if err != nil {
		return fmt.Errorf("marshal best config: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO public.recommendations
		  (job_id, best_engine, best_config, cost_per_mtok, tok_per_sec, reasoning, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())`,
		rec.JobID, rec.BestEngine, cfgJSON,
		rec.CostPerMTok, rec.TokPerSec, rec.Reasoning,
	)
	if err != nil {
		return fmt.Errorf("save recommendation: %w", err)
	}
	return nil
}

// AppendAuditLog inserts an append-only audit entry. Never updates or deletes.
func (s *Store) AppendAuditLog(ctx context.Context, tenantID, action, resourceID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO public.audit_log (id, tenant_id, action, resource_id, created_at)
		VALUES (uuid_generate_v4(), $1, $2, $3, NOW())`,
		tenantID, action, resourceID,
	)
	if err != nil {
		return fmt.Errorf("append audit log: %w", err)
	}
	return nil
}

