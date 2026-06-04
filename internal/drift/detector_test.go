package drift

import (
	"testing"
)

func TestCheckMetric(t *testing.T) {
	config := DetectorConfig{
		WarningThresholdPct:  15.0,
		CriticalThresholdPct: 30.0,
	}
	detector := &Detector{config: config}

	tests := []struct {
		name          string
		metricName    string
		baseline      float64
		current       float64
		expectedAlert bool
		expectedSev   string
	}{
		{
			name:          "throughput drop warning",
			metricName:    "tok_per_sec",
			baseline:      100,
			current:       80, // 20% drop
			expectedAlert: true,
			expectedSev:   "warning",
		},
		{
			name:          "throughput drop critical",
			metricName:    "tok_per_sec",
			baseline:      100,
			current:       60, // 40% drop
			expectedAlert: true,
			expectedSev:   "critical",
		},
		{
			name:          "throughput improvement no alert",
			metricName:    "tok_per_sec",
			baseline:      100,
			current:       120, // 20% improvement
			expectedAlert: false,
		},
		{
			name:          "memory increase warning",
			metricName:    "gpu_mem",
			baseline:      1000,
			current:       1200, // 20% increase
			expectedAlert: true,
			expectedSev:   "warning",
		},
		{
			name:          "memory decrease no alert",
			metricName:    "gpu_mem",
			baseline:      1000,
			current:       800, // 20% decrease
			expectedAlert: false,
		},
		{
			name:          "latency increase warning",
			metricName:    "ttft_p50",
			baseline:      50,
			current:       60, // 20% increase
			expectedAlert: true,
			expectedSev:   "warning",
		},
		{
			name:          "latency decrease no alert",
			metricName:    "ttft_p50",
			baseline:      50,
			current:       40, // 20% decrease (improvement)
			expectedAlert: false,
		},
		{
			name:          "small deviation no alert",
			metricName:    "tok_per_sec",
			baseline:      100,
			current:       90, // 10% drop (below 15% threshold)
			expectedAlert: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alert := detector.checkMetric("vllm", "test-model", tt.metricName, tt.baseline, tt.current)
			
			if tt.expectedAlert {
				if alert == nil {
					t.Errorf("expected alert, got nil")
					return
				}
				if alert.Severity != tt.expectedSev {
					t.Errorf("expected severity %s, got %s", tt.expectedSev, alert.Severity)
				}
				if alert.Metric != tt.metricName {
					t.Errorf("expected metric %s, got %s", tt.metricName, alert.Metric)
				}
			} else {
				if alert != nil {
					t.Errorf("expected no alert, got %+v", alert)
				}
			}
		})
	}
}

func TestCheckMetricZeroBaseline(t *testing.T) {
	config := DetectorConfig{
		WarningThresholdPct:  15.0,
		CriticalThresholdPct: 30.0,
	}
	detector := &Detector{config: config}

	alert := detector.checkMetric("vllm", "test-model", "tok_per_sec", 0, 100)
	if alert != nil {
		t.Errorf("expected no alert for zero baseline, got %+v", alert)
	}
}