import random
import time

from .base import BaseEngine, BenchmarkResult, Workload


class MockEngine(BaseEngine):
    """Fake engine that returns plausible metrics without a GPU."""

    def run_benchmark(self, workload: Workload) -> BenchmarkResult:
        time.sleep(0.1)  # simulate a short run
        return BenchmarkResult(
            ttft_ms=round(random.uniform(80, 200), 2),
            itl_ms=round(random.uniform(10, 30), 2),
            throughput_tok_per_s=round(random.uniform(800, 2000), 2),
            gpu_memory_mb=random.randint(10_000, 24_000),
            kv_cache_hit=round(random.uniform(0.4, 0.95), 3),
            error_rate=0.0,
            cost_per_mtok=round(random.uniform(0.1, 0.5), 4),
        )
