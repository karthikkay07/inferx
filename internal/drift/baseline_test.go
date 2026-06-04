package drift

import (
	"testing"
	"time"

	"github.com/inferbolthq/inferbolt/internal/jobs"
)

func TestComputeFromResults(t *testing.T) {
	store := &BaselineStore{} // pool not needed for this test

	tests := []struct {
		name           string
		results        []jobs.Result
		expectValid    bool
		expectedTTFT50 float64
		expectedTokSec float64
	}{
		{
			name:        "insufficient samples",
			results:     make([]jobs.Result, 4), // < 5
			expectValid: false,
		},
		{
			name: "valid computation",
			results: []jobs.Result{
				{TTFTP50Ms: 10, TTFTP99Ms: 50, TokPerSec: 100, GPUMemMB: 1000},
				{TTFTP50Ms: 20, TTFTP99Ms: 60, TokPerSec: 110, GPUMemMB: 1100},
				{TTFTP50Ms: 30, TTFTP99Ms: 70, TokPerSec: 120, GPUMemMB: 1200},
				{TTFTP50Ms: 40, TTFTP99Ms: 80, TokPerSec: 130, GPUMemMB: 1300},
				{TTFTP50Ms: 50, TTFTP99Ms: 90, TokPerSec: 140, GPUMemMB: 1400},
			},
			expectValid:    true,
			expectedTTFT50: 30, // median of [10, 20, 30, 40, 50]
			expectedTokSec: 120, // mean of [100, 110, 120, 130, 140]
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseline, valid := store.ComputeFromResults(nil, "vllm", "test-model", tt.results)
			if valid != tt.expectValid {
				t.Errorf("expected valid=%v, got %v", tt.expectValid, valid)
			}
			if tt.expectValid {
				if baseline.TTFTP50Ms != tt.expectedTTFT50 {
					t.Errorf("expected TTFT P50 %f, got %f", tt.expectedTTFT50, baseline.TTFTP50Ms)
				}
				if baseline.TokPerSec != tt.expectedTokSec {
					t.Errorf("expected TokPerSec %f, got %f", tt.expectedTokSec, baseline.TokPerSec)
				}
				if baseline.SampleCount != len(tt.results) {
					t.Errorf("expected sample count %d, got %d", len(tt.results), baseline.SampleCount)
				}
			}
		})
	}
}

func TestComputeMedian(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{
			name:     "odd length",
			values:   []float64{1, 2, 3, 4, 5},
			expected: 3,
		},
		{
			name:     "even length",
			values:   []float64{1, 2, 3, 4},
			expected: 2.5,
		},
		{
			name:     "single value",
			values:   []float64{42},
			expected: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeMedian(tt.values)
			if result != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}
}

func TestComputePercentile(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	
	tests := []struct {
		percentile float64
		expected   float64
	}{
		{0.5, 5.5},   // 50th percentile (median)
		{0.95, 9.55}, // 95th percentile
		{0.0, 1},     // 0th percentile (min)
		{1.0, 10},    // 100th percentile (max)
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := computePercentile(values, tt.percentile)
			if result != tt.expected {
				t.Errorf("percentile %f: expected %f, got %f", tt.percentile, tt.expected, result)
			}
		})
	}
}