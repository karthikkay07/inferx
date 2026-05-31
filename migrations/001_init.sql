CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS public.jobs (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    model           TEXT NOT NULL,
    engines         TEXT[] NOT NULL,
    workload_config JSONB NOT NULL,
    gpu_profile     TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT 'pending',
    run_id          TEXT,
    error_msg       TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS jobs_tenant_state ON public.jobs (tenant_id, state);
CREATE INDEX IF NOT EXISTS jobs_updated_at   ON public.jobs (updated_at DESC);

CREATE TABLE IF NOT EXISTS public.recommendations (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    job_id        TEXT NOT NULL REFERENCES public.jobs(id),
    best_engine   TEXT NOT NULL,
    best_config   JSONB NOT NULL,
    cost_per_mtok DOUBLE PRECISION,
    tok_per_sec   DOUBLE PRECISION,
    reasoning     TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS public.audit_log (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id   TEXT NOT NULL,
    action      TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS audit_log_tenant ON public.audit_log (tenant_id, created_at DESC);
