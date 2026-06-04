package drift

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/inferbolthq/inferbolt/internal/jobs"
)

type Baseline struct {
	Engine      string    `json:"engine"`
	Model       string    `json:"model"`
	TTFTP50Ms   float64   `json:"ttft_p50_ms"`
	TTFTP99Ms   float64   `json:"ttft_p99_ms"`
	TokPerSec   float64   `json:"tok_per_sec"`
	GPUMemMB    int64     `json:"gpu_mem_mb"`
	SetAt       time.Time `json:"set_at"`
	SampleCount int       `json:"sample_count"`
}

type BaselineStore struct {
	pool *pgxpool.Pool
}

func NewBaselineStore(pool *pgxpool.Pool) *BaselineStore {
	return &BaselineStore{pool: pool}
}

const upsertBaseline = `
INSERT INTO public.baselines
  (engine, model, ttft_p50_ms, ttft_p99_ms, tok_per_sec, gpu_mem_mb, sample_count, set_at)
VALUES
  (@engine, @model, @ttft_p50_ms, @ttft_p99_ms, @tok_per_sec, @gpu_mem_mb, @sample_count, @set_at)
ON CONFLICT (engine, model) DO UPDATE SET
  ttft_p50_ms = EXCLUDED.ttft_p50_ms,
  ttft_p99_ms = EXCLUDED.ttft_p99_ms,
  tok_per_sec = EXCLUDED.tok_per_sec,
  gpu_mem_mb = EXCLUDED.gpu_mem_mb,
  sample_count = EXCLUDED.sample_count,
  set_at = EXCLUDED.set_at`

func (b *BaselineStore) Set(ctx context.Context, baseline Baseline) error {
	_, err := b.pool.Exec(ctx, upsertBaseline, pgx.NamedArgs{
		"engine":       baseline.Engine,
		"model":        baseline.Model,
		"ttft_p50_ms":  baseline.TTFTP50Ms,
		"ttft_p99_ms":  baseline.TTFTP99Ms,
		"tok_per_sec":  baseline.TokPerSec,
		"gpu_mem_mb":   baseline.GPUMemMB,
		"sample_count": baseline.SampleCount,
		"set_at":       baseline.SetAt,
	})
	if err != nil {
		return fmt.Errorf("set baseline: %w", err)
	}
	return nil
}

const selectBaseline = `
SELECT engine, model, ttft_p50_ms, ttft_p99_ms, tok_per_sec, gpu_mem_mb, sample_count, set_at
FROM public.baselines
WHERE engine = $1 AND model = $2`

func (b *BaselineStore) Get(ctx context.Context, engine, model string) (*Baseline, error) {
	var baseline Baseline
	err := b.pool.QueryRow(ctx, selectBaseline, engine, model).Scan(
		&baseline.Engine,
		&baseline.Model,
		&baseline.TTFTP50Ms,
		&baseline.TTFTP99Ms,
		&baseline.TokPerSec,
		&baseline.GPUMemMB,
		&baseline.SampleCount,
		&baseline.SetAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get baseline: %w", err)
	}
	return &baseline, nil
}

func (b *BaselineStore) ComputeFromResults(ctx context.Context, engine, model string, results []jobs.Result) (Baseline, bool) {
	if len(results) < 5 {
		return Baseline{}, false
	}

	// Extract metrics for computation
	ttftP50Values := make([]float64, len(results))
	ttftP99Values := make([]float64, len(results))
	tokPerSecValues := make([]float64, len(results))
	gpuMemValues := make([]int64, len(results))

	for i, result := range results {
		ttftP50Values[i] = result.TTFTP50Ms
		ttftP99Values[i] = result.TTFTP99Ms
		tokPerSecValues[i] = result.TokPerSec
		gpuMemValues[i] = result.GPUMemMB
	}

	// Compute statistics
	ttftP50Median := computeMedian(ttftP50Values)
	ttftP99P95 := computePercentile(ttftP99Values, 0.95)
	tokPerSecMean := computeMean(tokPerSecValues)
	gpuMemMax := computeMaxInt64(gpuMemValues)

	return Baseline{
		Engine:      engine,
		Model:       model,
		TTFTP50Ms:   ttftP50Median,
		TTFTP99Ms:   ttftP99P95,
		TokPerSec:   tokPerSecMean,
		GPUMemMB:    gpuMemMax,
		SampleCount: len(results),
		SetAt:       time.Now(),
	}, true
}

func computeMedian(values []float64) float64 {
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

func computePercentile(values []float64, percentile float64) float64 {
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	
	n := len(sorted)
	index := percentile * float64(n-1)
	lower := int(index)
	upper := lower + 1
	
	if upper >= n {
		return sorted[n-1]
	}
	if lower < 0 {
		return sorted[0]
	}
	
	weight := index - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

func computeMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func computeMaxInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}