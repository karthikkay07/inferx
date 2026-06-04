# InferBolt Benchmark Results

Published benchmark reports comparing LLM inference engines on real hardware.
All benchmarks run using InferBolt on dedicated GPU nodes.

## Reports

| Model | Hardware | Date | Report |
|-------|----------|------|--------|
| Llama 3.1 8B | A100 80GB | TBD | [View](./llama3.1-8b-a100.md) |

## Methodology

- Each engine started fresh per benchmark run
- 200 requests per configuration
- Concurrency: 32 simultaneous requests
- Prompt: 512 tokens, Output: 256 tokens
- 3 runs averaged, outliers removed
- Results posted to TimescaleDB, reproducible via InferBolt CLI

## Reproduce these results

```bash
inferbolt benchmark compare \
  --model meta-llama/Llama-3.1-8B-Instruct \
  --engines vllm,sglang,llamacpp \
  --gpu a100-80gb
```
