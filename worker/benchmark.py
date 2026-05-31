"""
Worker entrypoint.

Reads a job from the orchestrator, runs the benchmark, emits results.

Usage:
    JOB_ID=abc123 ENGINE=mock python -m worker.benchmark
    JOB_ID=abc123 ENGINE=vllm MODEL=meta-llama/Llama-3-8B python -m worker.benchmark
"""

import os
import sys

import requests

from worker.engines import REGISTRY, Workload
from worker.engines.base import ORCHESTRATOR_URL
from worker.engines.vllm_engine import VLLMEngine


def fetch_job(job_id: str) -> dict:
    r = requests.get(f"{ORCHESTRATOR_URL}/jobs/{job_id}", timeout=10)
    r.raise_for_status()
    return r.json()


def main() -> None:
    job_id = os.environ["JOB_ID"]
    engine_name = os.environ.get("ENGINE", "mock")
    model = os.environ.get("MODEL", "mock-model")

    job = fetch_job(job_id)
    workload = Workload(**job["workload"])

    engine_cls = REGISTRY.get(engine_name)
    if engine_cls is None:
        print(f"Unknown engine '{engine_name}'. Available: {list(REGISTRY)}", file=sys.stderr)
        sys.exit(1)

    if engine_name == "vllm":
        engine = VLLMEngine(model=model)
        engine.start()
    else:
        engine = engine_cls()

    try:
        result = engine.run_benchmark(workload)
        engine.emit_result(result)
        print(f"Done: TTFT={result.ttft_ms}ms  throughput={result.throughput_tok_per_s:.1f} tok/s")
    finally:
        if engine_name == "vllm":
            engine.stop()


if __name__ == "__main__":
    main()
