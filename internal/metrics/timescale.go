package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/karthikkay07/inferx/internal/jobs"
)

type MetricsWriter struct {
	pool *pgxpool.Pool
}

func NewMetricsWriter(pool *pgxpool.Pool) *MetricsWriter {
	return &MetricsWriter{pool: pool}
}

const insertBenchResult = `
INSERT INTO metrics.bench_results
  (ts, job_id, engine, model, ttft_p50_ms, ttft_p99_ms, itl_ms,
   tok_per_s, gpu_mem_mb, kv_cache_hit, error_rate, cost_per_mtok, config)
VALUES
  (NOW(), @job_id, @engine, @model, @ttft_p50_ms, @ttft_p99_ms, @itl_ms,
   @tok_per_s, @gpu_mem_mb, @kv_cache_hit, @error_rate, @cost_per_mtok, @config)`

func (m *MetricsWriter) WriteBenchResult(ctx context.Context, r jobs.Result) error {
	configJSON, err := json.Marshal(r.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	_, err = m.pool.Exec(ctx, insertBenchResult, pgx.NamedArgs{
		"job_id":       r.JobID,
		"engine":       r.Engine,
		"model":        r.Model,
		"ttft_p50_ms":  r.TTFTP50Ms,
		"ttft_p99_ms":  r.TTFTP99Ms,
		"itl_ms":       r.ITLMs,
		"tok_per_s":    r.TokPerSec,
		"gpu_mem_mb":   r.GPUMemMB,
		"kv_cache_hit": r.KVCacheHit,
		"error_rate":   r.ErrorRate,
		"cost_per_mtok": r.CostPerMTok,
		"config":       configJSON,
	})
	if err != nil {
		return fmt.Errorf("write bench result: %w", err)
	}
	return nil
}

func (m *MetricsWriter) WriteBenchResultBatch(ctx context.Context, results []jobs.Result) error {
	batch := &pgx.Batch{}
	for _, r := range results {
		configJSON, err := json.Marshal(r.Config)
		if err != nil {
			return fmt.Errorf("marshal config for job %s: %w", r.JobID, err)
		}
		batch.Queue(insertBenchResult, pgx.NamedArgs{
			"job_id":       r.JobID,
			"engine":       r.Engine,
			"model":        r.Model,
			"ttft_p50_ms":  r.TTFTP50Ms,
			"ttft_p99_ms":  r.TTFTP99Ms,
			"itl_ms":       r.ITLMs,
			"tok_per_s":    r.TokPerSec,
			"gpu_mem_mb":   r.GPUMemMB,
			"kv_cache_hit": r.KVCacheHit,
			"error_rate":   r.ErrorRate,
			"cost_per_mtok": r.CostPerMTok,
			"config":       configJSON,
		})
	}
	br := m.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range results {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("batch exec bench result: %w", err)
		}
	}
	return nil
}

func (m *MetricsWriter) QueryByJob(ctx context.Context, jobID string) ([]jobs.Result, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT job_id, engine, model,
		       ttft_p50_ms, ttft_p99_ms, itl_ms, tok_per_s,
		       gpu_mem_mb, kv_cache_hit, error_rate, cost_per_mtok, config
		FROM metrics.bench_results
		WHERE job_id = $1
		ORDER BY ts ASC`, jobID)
	if err != nil {
		return nil, fmt.Errorf("query by job: %w", err)
	}
	defer rows.Close()
	return scanResults(rows)
}

func (m *MetricsWriter) QueryByEngineAndModel(ctx context.Context, engine, model string, since time.Time) ([]jobs.Result, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT job_id, engine, model,
		       ttft_p50_ms, ttft_p99_ms, itl_ms, tok_per_s,
		       gpu_mem_mb, kv_cache_hit, error_rate, cost_per_mtok, config
		FROM metrics.bench_results
		WHERE engine = $1 AND model = $2 AND ts > $3
		ORDER BY ts ASC`, engine, model, since)
	if err != nil {
		return nil, fmt.Errorf("query by engine/model: %w", err)
	}
	defer rows.Close()
	return scanResults(rows)
}

func scanResults(rows pgx.Rows) ([]jobs.Result, error) {
	var out []jobs.Result
	for rows.Next() {
		var r jobs.Result
		var configJSON []byte
		if err := rows.Scan(
			&r.JobID, &r.Engine, &r.Model,
			&r.TTFTP50Ms, &r.TTFTP99Ms, &r.ITLMs, &r.TokPerSec,
			&r.GPUMemMB, &r.KVCacheHit, &r.ErrorRate, &r.CostPerMTok,
			&configJSON,
		); err != nil {
			return nil, fmt.Errorf("scan bench result: %w", err)
		}
		if err := json.Unmarshal(configJSON, &r.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
