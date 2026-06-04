"""
Shared data models for the InferX benchmark worker.
"""

from pydantic import BaseModel


class WorkloadConfig(BaseModel):
    """Configuration for benchmark workload parameters."""
    concurrency: int = 32
    prompt_tokens: int = 512
    output_tokens: int = 256
    num_requests: int = 200


class EngineConfig(BaseModel):
    """Configuration for inference engine parameters."""
    quantization: str | None = None  # "fp8", "int4", "gptq", "awq"
    tensor_parallel: int = 1
    max_batch_size: int = 256
    max_model_len: int = 8192
    gpu_memory_utilization: float = 0.90


class BenchmarkJob(BaseModel):
    """A benchmark job request from the orchestrator."""
    job_id: str
    model: str
    engines: list[str]
    workload: WorkloadConfig
    gpu_profile: str
    tenant_id: str
    engine_config: EngineConfig = EngineConfig()


class RawRequestResult(BaseModel):
    """Raw timing and token data from a single inference request."""
    ttft_ms: float        # time to first token in milliseconds
    itl_ms: float         # inter-token latency in milliseconds
    total_ms: float       # total request duration
    output_tokens: int    # actual tokens generated
    error: str | None = None


class BenchmarkResult(BaseModel):
    """Aggregated benchmark results for one engine/model combination."""
    job_id: str
    run_id: str
    engine: str
    model: str
    ttft_p50_ms: float
    ttft_p99_ms: float
    itl_ms: float
    tok_per_s: float
    gpu_mem_mb: int
    kv_cache_hit: float
    error_rate: float
    cost_per_mtok: float
    config: dict


class RunStatus(BaseModel):
    """Status of a benchmark run."""
    run_id: str
    status: str   # "running", "completed", "failed"
    result: BenchmarkResult | None = None
    error: str | None = None


# Hardcoded GPU pricing per hour in USD
GPU_PRICING_PER_HOUR: dict[str, float] = {
    "a100-80gb": 3.50,
    "h100": 5.00,
    "l40s": 2.20,
    "cpu": 0.10,
}