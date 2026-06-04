package router

import (
	"testing"
)

func TestClassifyAgent(t *testing.T) {
	tests := []struct {
		name     string
		input    ClassificationInput
		expected ClassificationResult
	}{
		{
			name: "tool calls triggers agent workload",
			input: ClassificationInput{
				PromptTokens:      100,
				OutputTokens:      50,
				Concurrency:       10,
				StructuredOutput:  false,
				ToolCalls:         true,
				SharedPrefixRatio: 0.1,
			},
			expected: ClassificationResult{
				WorkloadType:      WorkloadAgent,
				RecommendedEngine: "sglang",
				Confidence:        0.95,
			},
		},
		{
			name: "structured output triggers agent workload",
			input: ClassificationInput{
				PromptTokens:      100,
				OutputTokens:      50,
				Concurrency:       10,
				StructuredOutput:  true,
				ToolCalls:         false,
				SharedPrefixRatio: 0.1,
			},
			expected: ClassificationResult{
				WorkloadType:      WorkloadAgent,
				RecommendedEngine: "sglang",
				Confidence:        0.95,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Classify(tt.input)
			if result.WorkloadType != tt.expected.WorkloadType {
				t.Errorf("expected workload type %s, got %s", tt.expected.WorkloadType, result.WorkloadType)
			}
			if result.RecommendedEngine != tt.expected.RecommendedEngine {
				t.Errorf("expected engine %s, got %s", tt.expected.RecommendedEngine, result.RecommendedEngine)
			}
			if result.Confidence != tt.expected.Confidence {
				t.Errorf("expected confidence %f, got %f", tt.expected.Confidence, result.Confidence)
			}
		})
	}
}

func TestClassifyRAG(t *testing.T) {
	input := ClassificationInput{
		PromptTokens:      100,
		OutputTokens:      50,
		Concurrency:       10,
		StructuredOutput:  false,
		ToolCalls:         false,
		SharedPrefixRatio: 0.5, // > 0.4
	}

	result := Classify(input)
	if result.WorkloadType != WorkloadRAG {
		t.Errorf("expected WorkloadRAG, got %s", result.WorkloadType)
	}
	if result.RecommendedEngine != "sglang" {
		t.Errorf("expected sglang, got %s", result.RecommendedEngine)
	}
}

func TestClassifyBatch(t *testing.T) {
	tests := []struct {
		name  string
		input ClassificationInput
	}{
		{
			name: "long prompts trigger batch",
			input: ClassificationInput{
				PromptTokens:      2000, // > 1024
				OutputTokens:      100,
				Concurrency:       10,
				StructuredOutput:  false,
				ToolCalls:         false,
				SharedPrefixRatio: 0.1,
			},
		},
		{
			name: "long outputs trigger batch",
			input: ClassificationInput{
				PromptTokens:      100,
				OutputTokens:      600, // > 512
				Concurrency:       10,
				StructuredOutput:  false,
				ToolCalls:         false,
				SharedPrefixRatio: 0.1,
			},
		},
		{
			name: "high concurrency triggers batch",
			input: ClassificationInput{
				PromptTokens:      100,
				OutputTokens:      100,
				Concurrency:       100, // > 50
				StructuredOutput:  false,
				ToolCalls:         false,
				SharedPrefixRatio: 0.1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Classify(tt.input)
			if result.WorkloadType != WorkloadBatch {
				t.Errorf("expected WorkloadBatch, got %s", result.WorkloadType)
			}
			if result.RecommendedEngine != "vllm" {
				t.Errorf("expected vllm, got %s", result.RecommendedEngine)
			}
		})
	}
}

func TestClassifyChat(t *testing.T) {
	input := ClassificationInput{
		PromptTokens:      100,  // <= 1024
		OutputTokens:      100,  // <= 512
		Concurrency:       10,   // <= 50
		StructuredOutput:  false,
		ToolCalls:         false,
		SharedPrefixRatio: 0.1,  // <= 0.4
	}

	result := Classify(input)
	if result.WorkloadType != WorkloadChat {
		t.Errorf("expected WorkloadChat, got %s", result.WorkloadType)
	}
	if result.RecommendedEngine != "vllm" {
		t.Errorf("expected vllm, got %s", result.RecommendedEngine)
	}
}