package jobs

import "time"

type JobState string

const (
	StatePending    JobState = "pending"
	StateRunning    JobState = "running"
	StateCollecting JobState = "collecting"
	StateAnalyzing  JobState = "analyzing"
	StateCompleted  JobState = "completed"
	StateFailed     JobState = "failed"
	StateCancelled  JobState = "cancelled"
)

type WorkloadConfig struct {
	Concurrency  int `json:"concurrency"`
	PromptTokens int `json:"prompt_tokens"`
	OutputTokens int `json:"output_tokens"`
	NumRequests  int `json:"num_requests"`
}

type Job struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenant_id"`
	Model          string         `json:"model"`
	Engines        []string       `json:"engines"`
	WorkloadConfig WorkloadConfig `json:"workload_config"`
	GPUProfile     string         `json:"gpu_profile"`
	State          JobState       `json:"state"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	ErrorMsg       string         `json:"error_msg,omitempty"`
	RunID          string         `json:"run_id,omitempty"`
}

type Result struct {
	JobID       string         `json:"job_id"`
	RunID       string         `json:"run_id"`
	Engine      string         `json:"engine"`
	Model       string         `json:"model"`
	TTFTP50Ms   float64        `json:"ttft_p50_ms"`
	TTFTP99Ms   float64        `json:"ttft_p99_ms"`
	ITLMs       float64        `json:"itl_ms"`
	TokPerSec   float64        `json:"tok_per_s"`
	GPUMemMB    int64          `json:"gpu_mem_mb"`
	KVCacheHit  float64        `json:"kv_cache_hit"`
	ErrorRate   float64        `json:"error_rate"`
	CostPerMTok float64        `json:"cost_per_mtok"`
	Config      map[string]any `json:"config"`
}

type Recommendation struct {
	JobID       string         `json:"job_id"`
	BestEngine  string         `json:"best_engine"`
	BestConfig  map[string]any `json:"best_config"`
	CostPerMTok float64        `json:"cost_per_mtok"`
	TokPerSec   float64        `json:"tok_per_sec"`
	Reasoning   string         `json:"reasoning"`
}

// validTransitions defines the allowed state machine edges.
var validTransitions = map[JobState][]JobState{
	StatePending:    {StateRunning},
	StateRunning:    {StateCollecting, StateFailed, StateCancelled},
	StateCollecting: {StateAnalyzing, StateFailed},
	StateAnalyzing:  {StateCompleted, StateFailed},
}

func ValidTransition(from, to JobState) bool {
	nexts, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range nexts {
		if s == to {
			return true
		}
	}
	return false
}

func IsTerminal(state JobState) bool {
	return state == StateCompleted || state == StateFailed || state == StateCancelled
}
