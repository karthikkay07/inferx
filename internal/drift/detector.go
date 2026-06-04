package drift

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/inferbolthq/inferbolt/internal/metrics"
)

type DriftAlert struct {
	Engine        string    `json:"engine"`
	Model         string    `json:"model"`
	Metric        string    `json:"metric"`
	BaselineValue float64   `json:"baseline_value"`
	CurrentValue  float64   `json:"current_value"`
	DeviationPct  float64   `json:"deviation_pct"`
	Severity      string    `json:"severity"`
	DetectedAt    time.Time `json:"detected_at"`
}

type DetectorConfig struct {
	WarningThresholdPct  float64       `json:"warning_threshold_pct"`
	CriticalThresholdPct float64       `json:"critical_threshold_pct"`
	CheckInterval        time.Duration `json:"check_interval"`
	LookbackWindow       time.Duration `json:"lookback_window"`
	MinSamples           int           `json:"min_samples"`
}

type Detector struct {
	config    DetectorConfig
	metrics   *metrics.MetricsWriter
	baselines *BaselineStore
	alertCh   chan DriftAlert
	db        *pgxpool.Pool
}

func NewDetector(config DetectorConfig, metrics *metrics.MetricsWriter, baselines *BaselineStore, db *pgxpool.Pool) *Detector {
	return &Detector{
		config:    config,
		metrics:   metrics,
		baselines: baselines,
		alertCh:   make(chan DriftAlert, 100),
		db:        db,
	}
}

func (d *Detector) Alerts() <-chan DriftAlert {
	return d.alertCh
}

func (d *Detector) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(d.config.CheckInterval)
		defer ticker.Stop()

		slog.Info("starting drift detector", 
			"check_interval", d.config.CheckInterval,
			"lookback_window", d.config.LookbackWindow,
			"min_samples", d.config.MinSamples)

		for {
			select {
			case <-ctx.Done():
				slog.Info("drift detector stopped")
				close(d.alertCh)
				return
			case <-ticker.C:
				slog.Debug("running drift check")
				if err := d.runCheck(ctx); err != nil {
					slog.Error("drift check failed", "error", err)
				}
			}
		}
	}()
}

func (d *Detector) runCheck(ctx context.Context) error {
	since := time.Now().Add(-d.config.LookbackWindow)

	// Query for distinct engine/model pairs with recent data
	pairs, err := d.getActiveEngineModelPairs(ctx, since)
	if err != nil {
		return fmt.Errorf("get active pairs: %w", err)
	}

	for _, pair := range pairs {
		if err := d.checkEngineModel(ctx, pair.Engine, pair.Model, since); err != nil {
			slog.Error("failed to check engine/model pair",
				"engine", pair.Engine,
				"model", pair.Model,
				"error", err)
			continue
		}
	}

	return nil
}

type EngineModelPair struct {
	Engine string
	Model  string
}

func (d *Detector) getActiveEngineModelPairs(ctx context.Context, since time.Time) ([]EngineModelPair, error) {
	rows, err := d.db.Query(ctx, `
		SELECT DISTINCT engine, model
		FROM metrics.bench_results
		WHERE ts > $1
		ORDER BY engine, model`, since)
	if err != nil {
		return nil, fmt.Errorf("query active pairs: %w", err)
	}
	defer rows.Close()

	var pairs []EngineModelPair
	for rows.Next() {
		var pair EngineModelPair
		if err := rows.Scan(&pair.Engine, &pair.Model); err != nil {
			return nil, fmt.Errorf("scan pair: %w", err)
		}
		pairs = append(pairs, pair)
	}

	return pairs, rows.Err()
}

func (d *Detector) checkEngineModel(ctx context.Context, engine, model string, since time.Time) error {
	// Get recent results for this engine/model
	results, err := d.metrics.QueryByEngineAndModel(ctx, engine, model, since)
	if err != nil {
		return fmt.Errorf("query results: %w", err)
	}

	// Skip if insufficient samples
	if len(results) < d.config.MinSamples {
		slog.Debug("insufficient samples for drift detection",
			"engine", engine,
			"model", model,
			"sample_count", len(results),
			"min_required", d.config.MinSamples)
		return nil
	}

	// Get or compute baseline
	baseline, err := d.baselines.Get(ctx, engine, model)
	if err != nil {
		return fmt.Errorf("get baseline: %w", err)
	}

	if baseline == nil {
		// No baseline exists, compute and set one
		if newBaseline, ok := d.baselines.ComputeFromResults(ctx, engine, model, results); ok {
			if err := d.baselines.Set(ctx, newBaseline); err != nil {
				return fmt.Errorf("set new baseline: %w", err)
			}
			slog.Info("computed new baseline",
				"engine", engine,
				"model", model,
				"sample_count", newBaseline.SampleCount)
		}
		return nil // Skip alerting on first baseline computation
	}

	// Compute current metrics from recent results
	if currentBaseline, ok := d.baselines.ComputeFromResults(ctx, engine, model, results); ok {
		// Check each metric for drift
		alerts := []*DriftAlert{
			d.checkMetric(engine, model, "ttft_p50", baseline.TTFTP50Ms, currentBaseline.TTFTP50Ms),
			d.checkMetric(engine, model, "ttft_p99", baseline.TTFTP99Ms, currentBaseline.TTFTP99Ms),
			d.checkMetric(engine, model, "tok_per_sec", baseline.TokPerSec, currentBaseline.TokPerSec),
			d.checkMetric(engine, model, "gpu_mem", float64(baseline.GPUMemMB), float64(currentBaseline.GPUMemMB)),
		}

		// Send non-nil alerts to channel
		for _, alert := range alerts {
			if alert != nil {
				select {
				case d.alertCh <- *alert:
					slog.Warn("drift alert generated",
						"engine", alert.Engine,
						"model", alert.Model,
						"metric", alert.Metric,
						"severity", alert.Severity,
						"deviation_pct", alert.DeviationPct)
				default:
					slog.Warn("alert channel full, dropping alert",
						"engine", alert.Engine,
						"model", alert.Model,
						"metric", alert.Metric)
				}
			}
		}
	}

	return nil
}

func (d *Detector) checkMetric(engine, model, metricName string, baselineVal, currentVal float64) *DriftAlert {
	if baselineVal == 0 {
		return nil // Cannot compute meaningful deviation
	}

	var deviationPct float64
	var shouldAlert bool

	switch metricName {
	case "tok_per_sec":
		// For throughput, negative deviation (drop in performance) is bad
		deviation := (currentVal - baselineVal) / baselineVal
		if deviation < 0 {
			deviationPct = math.Abs(deviation) * 100
			shouldAlert = true
		}
	case "gpu_mem":
		// For memory, positive deviation (increase in usage) is bad
		deviation := (currentVal - baselineVal) / baselineVal
		if deviation > 0 {
			deviationPct = deviation * 100
			shouldAlert = true
		}
	case "ttft_p50", "ttft_p99":
		// For latency metrics, any increase above threshold is bad
		deviation := (currentVal - baselineVal) / baselineVal
		if deviation > 0 {
			deviationPct = deviation * 100
			shouldAlert = true
		}
	}

	if !shouldAlert {
		return nil
	}

	// Determine severity
	var severity string
	if deviationPct >= d.config.CriticalThresholdPct {
		severity = "critical"
	} else if deviationPct >= d.config.WarningThresholdPct {
		severity = "warning"
	} else {
		return nil // Below warning threshold
	}

	return &DriftAlert{
		Engine:        engine,
		Model:         model,
		Metric:        metricName,
		BaselineValue: baselineVal,
		CurrentValue:  currentVal,
		DeviationPct:  deviationPct,
		Severity:      severity,
		DetectedAt:    time.Now(),
	}
}