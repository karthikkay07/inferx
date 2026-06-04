FROM python:3.11-slim

WORKDIR /app

RUN pip install uv

COPY pyproject.toml .
RUN uv sync --no-dev

COPY worker/ ./worker/

ENV PORT=8001
ENV LOG_LEVEL=INFO

CMD ["uv", "run", "uvicorn", "worker.main:app", "--host", "0.0.0.0", "--port", "8001"]
