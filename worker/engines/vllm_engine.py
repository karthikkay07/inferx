import subprocess
import time
import threading
import requests
from typing import Optional

from .base import BaseEngine, BenchmarkResult, Workload


VLLM_HOST = "http://127.0.0.1:8001"
VLLM_STARTUP_TIMEOUT = 120  # seconds


class VLLMEngine(BaseEngine):
    def __init__(self, model: str, gpu_memory_utilization: float = 0.9):
        self.model = model
        self.gpu_memory_utilization = gpu_memory_utilization
        self._proc: Optional[subprocess.Popen] = None

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    def start(self) -> None:
        self._proc = subprocess.Popen(
            [
                "python", "-m", "vllm.entrypoints.openai.api_server",
                "--model", self.model,
                "--port", "8001",
                "--gpu-memory-utilization", str(self.gpu_memory_utilization),
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
        )
        self._tail_logs()
        self._wait_ready()

    def stop(self) -> None:
        if self._proc:
            self._proc.terminate()
            self._proc.wait(timeout=30)
            self._proc = None

    # ------------------------------------------------------------------
    # Benchmark
    # ------------------------------------------------------------------

    def run_benchmark(self, workload: Workload) -> BenchmarkResult:
        prompt = "x " * workload.prompt_tokens  # synthetic prompt

        ttft_samples: list[float] = []
        itl_samples: list[float] = []
        errors = 0

        def single_request() -> None:
            nonlocal errors
            try:
                t0 = time.perf_counter()
                first_token = None
                tokens_received = 0

                with requests.post(
                    f"{VLLM_HOST}/v1/completions",
                    json={
                        "model": self.model,
                        "prompt": prompt,
                        "max_tokens": workload.completion_tokens,
                        "stream": True,
                    },
                    stream=True,
                    timeout=120,
                ) as resp:
                    resp.raise_for_status()
                    for chunk in resp.iter_lines():
                        if chunk and chunk != b"data: [DONE]":
                            now = time.perf_counter()
                            if first_token is None:
                                first_token = now
                                ttft_samples.append((first_token - t0) * 1000)
                            else:
                                itl_samples.append((now - first_token) * 1000)
                                first_token = now
                            tokens_received += 1
            except Exception:
                errors += 1

        deadline = time.monotonic() + workload.duration_secs
        while time.monotonic() < deadline:
            threads = [
                threading.Thread(target=single_request)
                for _ in range(workload.concurrency)
            ]
            for t in threads:
                t.start()
            for t in threads:
                t.join()

        total_requests = len(ttft_samples) + errors
        return BenchmarkResult(
            ttft_ms=_mean(ttft_samples),
            itl_ms=_mean(itl_samples),
            throughput_tok_per_s=_throughput(itl_samples, workload.duration_secs),
            gpu_memory_mb=self._gpu_memory_mb(),
            kv_cache_hit=self._kv_cache_hit(),
            error_rate=errors / max(total_requests, 1),
            cost_per_mtok=0.0,  # filled by cost/ module
        )

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _wait_ready(self) -> None:
        deadline = time.monotonic() + VLLM_STARTUP_TIMEOUT
        while time.monotonic() < deadline:
            try:
                r = requests.get(f"{VLLM_HOST}/health", timeout=2)
                if r.status_code == 200:
                    return
            except requests.ConnectionError:
                pass
            time.sleep(2)
        raise TimeoutError(f"vLLM did not become ready within {VLLM_STARTUP_TIMEOUT}s")

    def _tail_logs(self) -> None:
        def _stream():
            for line in self._proc.stdout:
                print("[vllm]", line.decode().rstrip())
        threading.Thread(target=_stream, daemon=True).start()

    def _gpu_memory_mb(self) -> int:
        try:
            r = requests.get(f"{VLLM_HOST}/metrics", timeout=2)
            for line in r.text.splitlines():
                if "vllm:gpu_cache_usage_perc" in line and not line.startswith("#"):
                    # rough proxy: not a real MB value, replace with nvidia-smi call for accuracy
                    return 0
        except Exception:
            pass
        return 0

    def _kv_cache_hit(self) -> float:
        try:
            r = requests.get(f"{VLLM_HOST}/metrics", timeout=2)
            for line in r.text.splitlines():
                if "vllm:cpu_prefix_cache_hit_rate" in line and not line.startswith("#"):
                    parts = line.split()
                    return float(parts[-1])
        except Exception:
            pass
        return 0.0


def _mean(values: list[float]) -> float:
    return sum(values) / len(values) if values else 0.0


def _throughput(itl_samples: list[float], duration_secs: int) -> float:
    if not itl_samples or duration_secs == 0:
        return 0.0
    return len(itl_samples) / duration_secs
