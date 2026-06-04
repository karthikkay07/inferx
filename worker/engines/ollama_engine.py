import json
import logging
import subprocess
import time

import httpx

from worker.engines.base import BaseEngine, EngineStartError
from worker.models import EngineConfig, RawRequestResult

logger = logging.getLogger(__name__)

_BASE_URL = "http://localhost:11434"
_STARTUP_TIMEOUT = 30


class OllamaEngine(BaseEngine):

    def __init__(self) -> None:
        self._model: str = ""

    def name(self) -> str:
        return "ollama"

    def start(self, model: str, config: EngineConfig) -> None:
        self._model = model

        # Pull model (no-op if already present)
        subprocess.run(["ollama", "pull", model], check=False)

        deadline = time.monotonic() + _STARTUP_TIMEOUT
        while time.monotonic() < deadline:
            try:
                with httpx.Client(timeout=httpx.Timeout(10.0)) as client:
                    r = client.get(f"{_BASE_URL}/api/tags")
                    if r.status_code == 200:
                        return
            except (httpx.ConnectError, httpx.ReadError, httpx.TimeoutException):
                pass
            time.sleep(5)

        raise EngineStartError(f"Ollama not reachable within {_STARTUP_TIMEOUT}s")

    def teardown(self) -> None:
        pass

    def get_gpu_memory_mb(self) -> int:
        return 0

    def get_kv_cache_hit_rate(self) -> float:
        return 0.0

    async def _send_request(self, prompt: str, max_tokens: int) -> RawRequestResult:
        try:
            t_start = time.perf_counter()
            ttft_ms = 0.0
            output_tokens = 0
            first_chunk = True

            async with httpx.AsyncClient(timeout=httpx.Timeout(30.0)) as client:
                async with client.stream(
                    "POST",
                    f"{_BASE_URL}/api/generate",
                    json={
                        "model": self._model,
                        "prompt": prompt,
                        "stream": True,
                        "options": {"num_predict": max_tokens},
                    },
                ) as resp:
                    async for line in resp.aiter_lines():
                        now = time.perf_counter()
                        if not line:
                            continue
                        try:
                            data = json.loads(line)
                        except json.JSONDecodeError:
                            continue
                        if first_chunk and data.get("response"):
                            ttft_ms = (now - t_start) * 1000
                            first_chunk = False
                        if data.get("done"):
                            output_tokens = data.get("eval_count", 0)
                            break

            total_ms = (time.perf_counter() - t_start) * 1000
            itl_ms = (total_ms - ttft_ms) / max(output_tokens - 1, 1) if output_tokens > 1 else 0.0
            return RawRequestResult(
                ttft_ms=ttft_ms,
                itl_ms=itl_ms,
                total_ms=total_ms,
                output_tokens=output_tokens,
            )
        except Exception as e:
            return RawRequestResult(ttft_ms=0.0, itl_ms=0.0, total_ms=0.0, output_tokens=0, error=str(e))
