from .base import BaseEngine, BenchmarkResult, Workload
from .mock_engine import MockEngine
from .vllm_engine import VLLMEngine

REGISTRY: dict[str, type[BaseEngine]] = {
    "vllm": VLLMEngine,
    "mock": MockEngine,
}

__all__ = ["BaseEngine", "BenchmarkResult", "Workload", "MockEngine", "VLLMEngine", "REGISTRY"]
