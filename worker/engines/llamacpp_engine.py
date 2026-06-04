import json
import logging
import re
import subprocess
import time
from pathlib import Path

import httpx

from worker.engines.base import BaseEngine, EngineStartError
from worker.models import EngineConfig, RawRequestResult

logger = logging.getLogger(__name__)

_PORT = 8102
_BASE_URL = f"http://localhost:{_PORT}"
_STARTUP_TIMEOUT = 60


def _model_slug(model: str) -> str:
    return re.sub(r"[^a-zA-Z0-9_-]", "_", model)


class LlamaCppEngine(BaseEngine):

    def __init__(self) -> None:
        self._proc: subprocess.Popen | None = None
        self._model: str = ""

    def name(self) -> str:
        return "llamacpp"

    def start(self, model: str, config: EngineConfig) -> None:
        self._model = model
        log_path = Path(f"/tmp/inferbolt-llamacpp-{_model_slug(model)}.log")
        cmd = [
            "llama-server",
            "--model", model,
            "--port", str(_PORT),
            "--ctx-size", str(config.max_model_len),
            "--n-gpu-layers", "999",
        ]

        log_file = log_path.open("w")
        self._proc = subprocess.Popen(cmd, stdout=log_file, stderr=log_file)

        deadline = time.monotonic() + _STARTUP_TIMEOUT
        while time.monotonic() < deadline:
            try:
                with httpx.Client(timeout=httpx.Timeout(10.0)) as client:
                    r = client.get(f"{_BASE_URL}/health")
                    if r.status_code == 200:
                        return
            except (httpx.ConnectError, httpx.ReadError, httpx.TimeoutException):
                pass
            time.sleep(5)

        self.teardown()
        raise EngineStartError(f"llama.cpp did not become ready within {_STARTUP_TIMEOUT}s")

    def teardown(self) -> None:
        try:
            if self._proc is None:
                return
            self._proc.terminate()
            try:
                self._proc.wait(timeout=30)
            except subprocess.TimeoutExpired:
                self._proc.kill()
            self._proc = None
        except Exception as e:
            logger.error("llama.cpp teardown error: %s", e)

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
                    f"{_BASE_URL}/completion",
                    json={
                        "prompt": prompt,
                        "n_predict": max_tokens,
                        "stream": True,
                    },
                ) as resp:
                    async for line in resp.aiter_lines():
                        now = time.perf_counter()
                        if not line.startswith("data:"):
                            continue
                        payload = line[5:].strip()
                        try:
                            data = json.loads(payload)
                        except json.JSONDecodeError:
                            continue
                        if first_chunk and data.get("content"):
                            ttft_ms = (now - t_start) * 1000
                            first_chunk = False
                        if data.get("stop"):
                            output_tokens = data.get("tokens_predicted", 0)
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
