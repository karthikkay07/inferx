CREATE TABLE IF NOT EXISTS public.baselines (
    engine          TEXT NOT NULL,
    model           TEXT NOT NULL,
    ttft_p50_ms     DOUBLE PRECISION NOT NULL,
    ttft_p99_ms     DOUBLE PRECISION NOT NULL,
    tok_per_sec     DOUBLE PRECISION NOT NULL,
    gpu_mem_mb      BIGINT NOT NULL,
    sample_count    INT NOT NULL,
    set_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (engine, model)
);

CREATE INDEX IF NOT EXISTS baselines_set_at ON public.baselines (set_at DESC);