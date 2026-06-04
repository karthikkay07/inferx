import pytest

from worker.cost.modeler import compute_cost_report
from worker.engines.base import BaseEngine
from worker.models import BenchmarkResult, EngineConfig, RawRequestResult


class MockEngine(BaseEngine):

    def name(self) -> str:
        return "mock"

    def start(self, model: str, config: EngineConfig) -> None:
        pass

    def teardown(self) -> None:
        pass

    def get_gpu_memory_mb(self) -> int:
        return 40960

    def get_kv_cache_hit_rate(self) -> float:
        return 0.72

    async def _send_request(self, prompt: str, max_tokens: int) -> RawRequestResult:
        return RawRequestResult(
            ttft_ms=150.0,
            itl_ms=25.0,
            total_ms=800.0,
            output_tokens=256,
        )


def _make_raw(count: int, error: str | None = None) -> list[RawRequestResult]:
    return [
        RawRequestResult(
            ttft_ms=150.0,
            itl_ms=25.0,
            total_ms=800.0,
            output_tokens=256,
            error=error,
        )
        for _ in range(count)
    ]


def test_compute_result_basic() -> None:
    engine = MockEngine()
    raw = _make_raw(100)
    config = EngineConfig()
    result = engine.compute_result("job1", "run1", "test-model", "a100-80gb", raw, config)

    assert result.ttft_p50_ms == pytest.approx(150.0)
    assert result.ttft_p99_ms == pytest.approx(150.0)
    assert result.error_rate == pytest.approx(0.0)
    assert result.gpu_mem_mb == 40960
    assert result.kv_cache_hit == pytest.approx(0.72)
    assert result.cost_per_mtok > 0


def test_compute_result_with_errors() -> None:
    engine = MockEngine()
    raw = _make_raw(90) + _make_raw(10, error="timeout")
    config = EngineConfig()
    result = engine.compute_result("job1", "run1", "test-model", "a100-80gb", raw, config)

    assert result.error_rate == pytest.approx(0.1)


def test_cost_report_cheaper_than_gpt4o() -> None:
    from worker.models import GPU_PRICING_PER_HOUR

    tok_per_s = 1000.0
    gpu_profile = "a100-80gb"
    gpu_price = GPU_PRICING_PER_HOUR[gpu_profile]
    cost_per_mtok = (gpu_price / 3600) / (tok_per_s / 1_000_000)

    result = BenchmarkResult(
        job_id="job1",
        run_id="run1",
        engine="vllm",
        model="test-model",
        ttft_p50_ms=150.0,
        ttft_p99_ms=200.0,
        itl_ms=25.0,
        tok_per_s=tok_per_s,
        gpu_mem_mb=40960,
        kv_cache_hit=0.72,
        error_rate=0.0,
        cost_per_mtok=cost_per_mtok,
        config={},
    )

    report = compute_cost_report(result, gpu_profile)
    assert report.vs_openai_gpt4o < 1.0
