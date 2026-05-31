CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE SCHEMA IF NOT EXISTS metrics;

CREATE TABLE IF NOT EXISTS metrics.bench_results (
    ts            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    job_id        TEXT NOT NULL,
    engine        TEXT NOT NULL,
    model         TEXT NOT NULL,
    ttft_p50_ms   DOUBLE PRECISION,
    ttft_p99_ms   DOUBLE PRECISION,
    itl_ms        DOUBLE PRECISION,
    tok_per_s     DOUBLE PRECISION,
    gpu_mem_mb    BIGINT,
    kv_cache_hit  DOUBLE PRECISION,
    error_rate    DOUBLE PRECISION,
    cost_per_mtok DOUBLE PRECISION,
    config        JSONB
);

SELECT create_hypertable('metrics.bench_results', 'ts', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS bench_results_job_id       ON metrics.bench_results (job_id, ts DESC);
CREATE INDEX IF NOT EXISTS bench_results_engine_model ON metrics.bench_results (engine, model, ts DESC);

ALTER TABLE metrics.bench_results SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'engine,model'
);

SELECT add_compression_policy('metrics.bench_results', INTERVAL '7 days');
