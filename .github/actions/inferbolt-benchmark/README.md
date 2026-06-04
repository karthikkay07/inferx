# InferBolt Benchmark Action

Run LLM inference benchmarks in CI and automatically detect performance regressions.

## Usage

```yaml
- uses: inferbolthq/inferbolt/.github/actions/inferbolt-benchmark@v1
  with:
    server-url: ${{ secrets.INFERBOLT_SERVER_URL }}
    api-key: ${{ secrets.INFERBOLT_API_KEY }}
    model: meta-llama/Llama-3.1-8B-Instruct
    engines: vllm,sglang
    gpu-profile: a100-80gb
    fail-on-regression: true
    regression-threshold-pct: 15
```

## What it does

1. Installs the inferbolt CLI
2. Submits a benchmark job to your InferBolt server
3. Waits for results
4. Compares against 7-day baseline
5. Fails the workflow if tok/s drops more than threshold

## Outputs

| Output | Description |
|--------|-------------|
| `job-id` | InferBolt job ID for this run |
| `tok-per-s` | Tokens per second achieved |
| `cost-per-mtok` | Cost per million tokens |
| `regression-detected` | `true` if regression found |
