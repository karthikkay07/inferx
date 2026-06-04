import asyncio
from abc import ABC, abstractmethod

import numpy as np

from worker.models import (
    BenchmarkResult,
    EngineConfig,
    GPU_PRICING_PER_HOUR,
    RawRequestResult,
    WorkloadConfig,
)

_VOCAB = (
    "the quick brown fox jumps over the lazy dog "
    "lorem ipsum dolor sit amet consectetur adipiscing elit "
    "sed do eiusmod tempor incididunt ut labore et dolore magna aliqua"
).split()


def _generate_prompt(token_count: int) -> str:
    chars_needed = int(token_count * 1.3)
    words: list[str] = []
    length = 0
    i = 0
    while length < chars_needed:
        word = _VOCAB[i % len(_VOCAB)]
        words.append(word)
        length += len(word) + 1
        i += 1
    return " ".join(words)


class EngineStartError(Exception):
    pass


class EngineNotReadyError(Exception):
    pass


class BaseEngine(ABC):

    @abstractmethod
    def name(self) -> str: ...

    @abstractmethod
    def start(self, model: str, config: EngineConfig) -> None: ...

    @abstractmethod
    def teardown(self) -> None: ...

    @abstractmethod
    def get_gpu_memory_mb(self) -> int: ...

    @abstractmethod
    def get_kv_cache_hit_rate(self) -> float: ...

    @abstractmethod
    async def _send_request(self, prompt: str, max_tokens: int) -> RawRequestResult: ...

    async def benchmark(self, model: str, workload: WorkloadConfig) -> list[RawRequestResult]:
        sem = asyncio.Semaphore(workload.concurrency)
        prompts = [_generate_prompt(workload.prompt_tokens) for _ in range(workload.num_requests)]

        async def bounded(prompt: str) -> RawRequestResult:
            async with sem:
                return await self._send_request(prompt, workload.output_tokens)

        return list(await asyncio.gather(*[bounded(p) for p in prompts]))

    def compute_result(
        self,
        job_id: str,
        run_id: str,
        model: str,
        gpu_profile: str,
        raw: list[RawRequestResult],
        config: EngineConfig,
    ) -> BenchmarkResult:
        successful = [r for r in raw if r.error is None]
        failed_count = len(raw) - len(successful)

        if successful:
            ttft_arr = np.array([r.ttft_ms for r in successful])
            itl_arr = np.array([r.itl_ms for r in successful])
            ttft_p50 = float(np.percentile(ttft_arr, 50))
            ttft_p99 = float(np.percentile(ttft_arr, 99))
            itl_mean = float(np.mean(itl_arr))
            total_tokens = sum(r.output_tokens for r in successful)
            total_s = sum(r.total_ms for r in successful) / 1000.0
            tok_per_s = total_tokens / total_s if total_s > 0 else 0.0
        else:
            ttft_p50 = ttft_p99 = itl_mean = tok_per_s = 0.0

        error_rate = failed_count / len(raw) if raw else 0.0
        gpu_price = GPU_PRICING_PER_HOUR.get(gpu_profile, 0.10)
        cost_per_mtok = (gpu_price / 3600) / (tok_per_s / 1_000_000) if tok_per_s > 0 else 0.0

        return BenchmarkResult(
            job_id=job_id,
            run_id=run_id,
            engine=self.name(),
            model=model,
            ttft_p50_ms=ttft_p50,
            ttft_p99_ms=ttft_p99,
            itl_ms=itl_mean,
            tok_per_s=tok_per_s,
            gpu_mem_mb=self.get_gpu_memory_mb(),
            kv_cache_hit=self.get_kv_cache_hit_rate(),
            error_rate=error_rate,
            cost_per_mtok=cost_per_mtok,
            config=config.model_dump(),
        )
