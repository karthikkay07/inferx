package engine

import (
	"context"
	"time"
)

type Engine interface {
	Name() string
	Deploy(ctx context.Context, cfg Config) error
	Benchmark(ctx context.Context, w Workload) (Result, error)
	Metrics(ctx context.Context) (EngineMetrics, error)
	Teardown(ctx context.Context) error
}

type Config struct {
	Model    string
	GPUCount int
	ExtraEnv map[string]string
}

type Workload struct {
	PromptTokens     int
	CompletionTokens int
	Concurrency      int
	DurationSecs     int
}

type Result struct {
	TTFT       time.Duration
	ITL        time.Duration // inter-token latency
	Throughput float64       // tokens/sec
	GPUMemoryMB int
	KVCacheHit float64 // 0-1
	ErrorRate  float64
	CostPerMTok float64
}

type EngineMetrics struct {
	GPUUtilPct  float64
	MemUsedMB   int64
	RequestQueue int
}
