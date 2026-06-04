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

_PORT = 8101
_BASE_URL = f"http://localhost:{_PORT}"
_STARTUP_TIMEOUT = 600  # 10 minutes — model download + load on first run


def _model_slug(model: str) -> str:
    return re.sub(r"[^a-zA-Z0-9_-]", "_", model)


class SGLangEngine(BaseEngine):

    def __init__(self) -> None:
        self._proc: subprocess.Popen | None = None
        self._model: str = ""

    def name(self) -> str:
        return "sglang"

    def start(self, model: str, config: EngineConfig) -> None:
        self._model = model
        log_path = Path(f"/tmp/inferbolt-sglang-{_model_slug(model)}.log")
        cmd = [
            "python", "-m", "sglang.launch_server",
            "--model-path", model,
            "--port", str(_PORT),
        ]
        if config.quantization:
            cmd += ["--quantization", config.quantization]
        if config.tensor_parallel > 1:
            cmd += ["--tp", str(config.tensor_parallel)]

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
        raise EngineStartError(f"SGLang did not become ready within {_STARTUP_TIMEOUT}s")

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
            logger.error("SGLang teardown error: %s", e)

    def get_gpu_memory_mb(self) -> int:
        try:
            with httpx.Client(timeout=httpx.Timeout(10.0)) as client:
                r = client.get(f"{_BASE_URL}/get_server_info")
                data = r.json()
                return int(data.get("memory_used_mb", 0))
        except Exception:
            pass
        return 0

    def get_kv_cache_hit_rate(self) -> float:
        try:
            with httpx.Client(timeout=httpx.Timeout(10.0)) as client:
                r = client.get(f"{_BASE_URL}/get_server_info")
                data = r.json()
                return float(data.get("cache_hit_rate", 0.0))
        except Exception:
            pass
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
                    f"{_BASE_URL}/v1/completions",
                    json={
                        "model": self._model,
                        "prompt": prompt,
                        "max_tokens": max_tokens,
                        "stream": True,
                        "stream_options": {"include_usage": True},
                    },
                ) as resp:
                    async for line in resp.aiter_lines():
                        now = time.perf_counter()
                        if not line.startswith("data:"):
                            continue
                        payload = line[5:].strip()
                        if payload == "[DONE]":
                            break
                        if first_chunk:
                            ttft_ms = (now - t_start) * 1000
                            first_chunk = False
                        try:
                            data = json.loads(payload)
                            usage = data.get("usage") or {}
                            if usage.get("completion_tokens"):
                                output_tokens = usage["completion_tokens"]
                        except (json.JSONDecodeError, KeyError):
                            pass

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
