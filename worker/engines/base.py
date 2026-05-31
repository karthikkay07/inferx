import os
from abc import ABC, abstractmethod

import requests
from pydantic import BaseModel

ORCHESTRATOR_URL = os.getenv("ORCHESTRATOR_URL", "http://localhost:8080")


class Workload(BaseModel):
    prompt_tokens: int
    completion_tokens: int
    concurrency: int
    duration_secs: int


class BenchmarkResult(BaseModel):
    ttft_ms: float
    itl_ms: float
    throughput_tok_per_s: float
    gpu_memory_mb: int
    kv_cache_hit: float
    error_rate: float
    cost_per_mtok: float


class BaseEngine(ABC):
    @abstractmethod
    def run_benchmark(self, workload: Workload) -> BenchmarkResult: ...

    def emit_result(self, result: BenchmarkResult) -> None:
        requests.post(f"{ORCHESTRATOR_URL}/results", json=result.model_dump())
