import logging

import optuna

from pydantic import BaseModel

from worker.engines.base import BaseEngine
from worker.models import EngineConfig, WorkloadConfig

logger = logging.getLogger(__name__)

optuna.logging.set_verbosity(optuna.logging.WARNING)


class SearchSpace(BaseModel):
    quantizations: list[str] = ["fp8", "int4", ""]
    tensor_parallels: list[int] = [1, 2, 4]
    max_batch_sizes: list[int] = [64, 128, 256, 512]
    n_trials: int = 20


class SearchResult(BaseModel):
    best_config: EngineConfig
    best_tok_per_s: float
    best_cost_per_mtok: float
    pareto_configs: list[dict]
    n_trials_run: int


class ConfigSearcher:

    def __init__(
        self,
        engine: BaseEngine,
        model: str,
        workload: WorkloadConfig,
        gpu_profile: str,
    ) -> None:
        self.engine = engine
        self.model = model
        self.workload = workload
        self.gpu_profile = gpu_profile

    async def run(self, space: SearchSpace) -> SearchResult:
        study = optuna.create_study(
            direction="maximize",
            sampler=optuna.samplers.TPESampler(),
        )

        trial_results: list[tuple[EngineConfig, float, float]] = []

        for _ in range(space.n_trials):
            trial = study.ask()
            quantization = trial.suggest_categorical("quantization", space.quantizations) or None
            tensor_parallel = trial.suggest_categorical("tensor_parallel", space.tensor_parallels)
            max_batch_size = trial.suggest_categorical("max_batch_size", space.max_batch_sizes)

            config = EngineConfig(
                quantization=quantization,
                tensor_parallel=tensor_parallel,
                max_batch_size=max_batch_size,
            )

            try:
                self.engine.start(self.model, config)
                raw = await self.engine.benchmark(self.model, self.workload)
                result = self.engine.compute_result(
                    "search", "search", self.model, self.gpu_profile, raw, config
                )
                self.engine.teardown()
                study.tell(trial, result.tok_per_s)
                trial_results.append((config, result.tok_per_s, result.cost_per_mtok))
                logger.info(
                    "trial complete",
                    extra={
                        "tok_per_s": result.tok_per_s,
                        "cost_per_mtok": result.cost_per_mtok,
                        "config": config.model_dump(),
                    },
                )
            except Exception as e:
                logger.error("trial failed: %s", e)
                try:
                    self.engine.teardown()
                except Exception:
                    pass
                study.tell(trial, 0.0)

        pareto = _pareto_frontier(trial_results)

        if trial_results:
            best = max(trial_results, key=lambda t: t[1])
            best_config, best_tok_per_s, best_cost = best
        else:
            best_config = EngineConfig()
            best_tok_per_s = 0.0
            best_cost = 0.0

        return SearchResult(
            best_config=best_config,
            best_tok_per_s=best_tok_per_s,
            best_cost_per_mtok=best_cost,
            pareto_configs=pareto,
            n_trials_run=len(trial_results),
        )


def _pareto_frontier(results: list[tuple[EngineConfig, float, float]]) -> list[dict]:
    pareto: list[dict] = []
    for i, (config, tok_per_s, cost) in enumerate(results):
        dominated = False
        for j, (_, other_tok, other_cost) in enumerate(results):
            if i == j:
                continue
            if other_tok >= tok_per_s and other_cost <= cost:
                if other_tok > tok_per_s or other_cost < cost:
                    dominated = True
                    break
        if not dominated:
            pareto.append({
                "config": config.model_dump(),
                "tok_per_s": tok_per_s,
                "cost_per_mtok": cost,
            })
    return pareto
