import asyncio
import json
import logging
import os
import socket
import uuid
from contextlib import asynccontextmanager

import httpx
from fastapi import FastAPI, HTTPException

from worker.cost.modeler import compute_cost_report
from worker.engines import get_engine
from worker.models import BenchmarkJob, BenchmarkResult, RunStatus

# ── Environment ──────────────────────────────────────────────────────────────

ORCHESTRATOR_URL = os.environ.get("ORCHESTRATOR_URL", "")
PORT = int(os.environ.get("PORT", "8001"))
OTEL_ENDPOINT = os.environ.get("OTEL_ENDPOINT", "")
LOG_LEVEL = os.environ.get("LOG_LEVEL", "INFO").upper()

WORKER_ID = str(uuid.uuid4())

# ── Logging ───────────────────────────────────────────────────────────────────


class _JSONFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        data: dict = {
            "ts": self.formatTime(record, self.datefmt),
            "level": record.levelname,
            "msg": record.getMessage(),
        }
        if record.exc_info:
            data["exc"] = self.formatException(record.exc_info)
        extra = {
            k: v
            for k, v in record.__dict__.items()
            if k not in logging.LogRecord.__dict__ and not k.startswith("_")
        }
        data.update(extra)
        return json.dumps(data)


_handler = logging.StreamHandler()
_handler.setFormatter(_JSONFormatter())
logging.basicConfig(level=getattr(logging, LOG_LEVEL, logging.INFO), handlers=[_handler], force=True)
logger = logging.getLogger(__name__)

# ── OTel (optional) ───────────────────────────────────────────────────────────

if OTEL_ENDPOINT:
    try:
        from opentelemetry import trace
        from opentelemetry.sdk.trace import TracerProvider
        from opentelemetry.sdk.trace.export import BatchSpanProcessor
        from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter

        _provider = TracerProvider()
        _provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter(endpoint=OTEL_ENDPOINT)))
        trace.set_tracer_provider(_provider)
    except ImportError:
        logger.warning("OTel packages not installed — tracing disabled")

# ── State ─────────────────────────────────────────────────────────────────────

_runs: dict[str, RunStatus] = {}

# ── Startup / shutdown ────────────────────────────────────────────────────────


async def _register_with_orchestrator() -> None:
    if not ORCHESTRATOR_URL:
        logger.warning("ORCHESTRATOR_URL not set — skipping registration")
        return

    hostname = socket.gethostname()
    payload = {
        "id": WORKER_ID,
        "url": f"http://{hostname}:{PORT}",
        "gpu_type": os.environ.get("GPU_PROFILE", "cpu"),
        "status": "idle",
    }

    for attempt in range(5):
        try:
            async with httpx.AsyncClient(timeout=httpx.Timeout(10.0)) as client:
                r = await client.post(
                    f"{ORCHESTRATOR_URL}/internal/workers/register",
                    json=payload,
                )
                r.raise_for_status()
                logger.info("registered with orchestrator", extra={"worker_id": WORKER_ID})
                return
        except Exception as e:
            logger.warning(
                "orchestrator registration attempt failed",
                extra={"attempt": attempt + 1, "error": str(e)},
            )
            if attempt < 4:
                await asyncio.sleep(3)

    logger.error("failed to register with orchestrator after 5 attempts")


@asynccontextmanager
async def lifespan(app: FastAPI):
    logger.info("worker starting", extra={"port": PORT, "worker_id": WORKER_ID})
    await _register_with_orchestrator()
    yield
    logger.info("worker stopping", extra={"worker_id": WORKER_ID})


# ── App ───────────────────────────────────────────────────────────────────────

app = FastAPI(title="inferbolt-worker", lifespan=lifespan)

if OTEL_ENDPOINT:
    try:
        from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
        FastAPIInstrumentor.instrument_app(app)
    except ImportError:
        pass

# ── Routes ────────────────────────────────────────────────────────────────────


@app.post("/run")
async def post_run(job: BenchmarkJob) -> dict:
    run_id = str(uuid.uuid4())
    _runs[run_id] = RunStatus(run_id=run_id, status="running")
    asyncio.create_task(_execute_benchmark(job, run_id))
    return {"run_id": run_id}


@app.get("/run/{run_id}/status")
async def get_run_status(run_id: str) -> RunStatus:
    status = _runs.get(run_id)
    if status is None:
        raise HTTPException(status_code=404, detail="run not found")
    return status


@app.get("/health")
async def health() -> dict:
    in_progress = sum(1 for r in _runs.values() if r.status == "running")
    return {"status": "ok", "runs_in_progress": in_progress}


@app.post("/internal/workers/register")
async def register() -> dict:
    return {"status": "ok"}


# ── Background task ───────────────────────────────────────────────────────────


async def _execute_benchmark(job: BenchmarkJob, run_id: str) -> None:
    last_result: BenchmarkResult | None = None
    engine = None

    try:
        for engine_name in job.engines:
            engine = get_engine(engine_name)
            try:
                engine.start(job.model, job.engine_config)
                raw = await engine.benchmark(job.model, job.workload)
                result = engine.compute_result(
                    job.job_id, run_id, job.model, job.gpu_profile, raw, job.engine_config
                )
                cost_report = compute_cost_report(result, job.gpu_profile)

                if ORCHESTRATOR_URL:
                    try:
                        async with httpx.AsyncClient(timeout=httpx.Timeout(30.0)) as client:
                            await client.post(
                                f"{ORCHESTRATOR_URL}/internal/results",
                                json=result.model_dump(),
                            )
                    except Exception as e:
                        logger.error(
                            "failed to post result to orchestrator",
                            extra={"error": str(e)},
                        )

                logger.info(
                    "benchmark complete",
                    extra={
                        "engine": engine_name,
                        "tok_per_s": result.tok_per_s,
                        "cost_per_mtok": cost_report.cost_per_mtok,
                    },
                )
                last_result = result
            finally:
                engine.teardown()

        _runs[run_id] = RunStatus(run_id=run_id, status="completed", result=last_result)
    except Exception as e:
        logger.error("benchmark task failed", extra={"run_id": run_id, "error": str(e)})
        _runs[run_id] = RunStatus(run_id=run_id, status="failed", error=str(e))
