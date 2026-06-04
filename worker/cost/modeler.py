from pydantic import BaseModel

from worker.models import BenchmarkResult, GPU_PRICING_PER_HOUR

OPENAI_PRICING_PER_MTOK: dict[str, float] = {
    "gpt-4o": 5.00,
    "gpt-4o-mini": 0.60,
}


class CostReport(BaseModel):
    engine: str
    model: str
    gpu_profile: str
    config: dict
    tok_per_s: float
    cost_per_mtok: float
    monthly_cost_at_1b_tokens: float
    vs_openai_gpt4o: float
    vs_openai_gpt4o_mini: float
    break_even_utilization: float


def compute_cost_report(result: BenchmarkResult, gpu_profile: str) -> CostReport:
    monthly_cost_at_1b_tokens = result.cost_per_mtok * 1000

    vs_gpt4o = result.cost_per_mtok / OPENAI_PRICING_PER_MTOK["gpt-4o"]
    vs_gpt4o_mini = result.cost_per_mtok / OPENAI_PRICING_PER_MTOK["gpt-4o-mini"]

    gpu_price = GPU_PRICING_PER_HOUR.get(gpu_profile, 0.10)
    gpt4o_mini_price = OPENAI_PRICING_PER_MTOK["gpt-4o-mini"]
    if result.tok_per_s > 0:
        break_even = (gpu_price / (gpt4o_mini_price * result.tok_per_s * 3.6)) * 100
        break_even = max(0.0, min(100.0, break_even))
    else:
        break_even = 100.0

    return CostReport(
        engine=result.engine,
        model=result.model,
        gpu_profile=gpu_profile,
        config=result.config,
        tok_per_s=result.tok_per_s,
        cost_per_mtok=result.cost_per_mtok,
        monthly_cost_at_1b_tokens=monthly_cost_at_1b_tokens,
        vs_openai_gpt4o=vs_gpt4o,
        vs_openai_gpt4o_mini=vs_gpt4o_mini,
        break_even_utilization=break_even,
    )
