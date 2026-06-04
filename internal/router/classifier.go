package router

import "fmt"

type WorkloadType string

const (
	WorkloadChat  WorkloadType = "chat"
	WorkloadBatch WorkloadType = "batch"
	WorkloadRAG   WorkloadType = "rag"
	WorkloadAgent WorkloadType = "agent"
)

type ClassificationInput struct {
	PromptTokens      int     `json:"prompt_tokens"`
	OutputTokens      int     `json:"output_tokens"`
	Concurrency       int     `json:"concurrency"`
	StructuredOutput  bool    `json:"structured_output"`
	ToolCalls         bool    `json:"tool_calls"`
	SharedPrefixRatio float64 `json:"shared_prefix_ratio"`
}

type ClassificationResult struct {
	WorkloadType      WorkloadType `json:"workload_type"`
	RecommendedEngine string       `json:"recommended_engine"`
	Reasoning         string       `json:"reasoning"`
	Confidence        float64      `json:"confidence"`
}

func Classify(input ClassificationInput) ClassificationResult {
	var matchedConditions int

	if input.ToolCalls || input.StructuredOutput {
		matchedConditions++
		return ClassificationResult{
			WorkloadType:      WorkloadAgent,
			RecommendedEngine: "sglang",
			Reasoning:         "SGLang provides superior constrained decoding with guided generation for structured outputs and tool calls, enabling 2-4x faster generation than vLLM for JSON schema validation and function calling patterns",
			Confidence:        calculateConfidence(matchedConditions),
		}
	}

	if input.SharedPrefixRatio > 0.4 {
		matchedConditions++
		return ClassificationResult{
			WorkloadType:      WorkloadRAG,
			RecommendedEngine: "sglang",
			Reasoning:         fmt.Sprintf("SGLang's RadixAttention reuses shared KV cache across requests with >40%% common prefix, reducing memory bandwidth by up to 60%% compared to vLLM for this workload pattern with %.1f%% shared prefix ratio", input.SharedPrefixRatio*100),
			Confidence:        calculateConfidence(matchedConditions),
		}
	}

	if input.PromptTokens > 1024 || input.OutputTokens > 512 {
		matchedConditions++
		return ClassificationResult{
			WorkloadType:      WorkloadBatch,
			RecommendedEngine: "vllm",
			Reasoning:         "vLLM's PagedAttention and continuous batching optimizes throughput for long sequences, achieving 20-24x higher throughput than naive batching for prompts >1024 tokens or outputs >512 tokens",
			Confidence:        calculateConfidence(matchedConditions),
		}
	}

	if input.Concurrency > 50 {
		matchedConditions++
		return ClassificationResult{
			WorkloadType:      WorkloadBatch,
			RecommendedEngine: "vllm",
			Reasoning:         "vLLM's continuous batching dynamically adjusts batch sizes for high concurrency workloads, maintaining 85%+ GPU utilization with >50 concurrent requests compared to static batching approaches",
			Confidence:        calculateConfidence(matchedConditions),
		}
	}

	return ClassificationResult{
		WorkloadType:      WorkloadChat,
		RecommendedEngine: "vllm",
		Reasoning:         "vLLM's optimized CUDA kernels and speculative decoding provide minimal time-to-first-token (TTFT) latency for interactive chat workloads, typically 40-60ms faster than other engines for sub-1024 token prompts",
		Confidence:        calculateConfidence(matchedConditions),
	}
}

func calculateConfidence(matchedConditions int) float64 {
	switch matchedConditions {
	case 0, 1:
		return 0.95
	case 2:
		return 0.75
	default:
		return 0.6
	}
}