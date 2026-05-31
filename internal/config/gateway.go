package config

import (
	"os"
	"strconv"
	"strings"
)

type GatewayConfig struct {
	HTTPAddr        string
	GRPCAddr        string
	JWTSecret       []byte
	APIKeys         map[string]struct{}
	RateLimit       RateLimitConfig
	OrchestratorURL string
}

type RateLimitConfig struct {
	RPS   float64
	Burst int
}

func LoadGateway() GatewayConfig {
	keys := map[string]struct{}{}
	for _, k := range strings.Split(getenv("API_KEYS", ""), ",") {
		if k = strings.TrimSpace(k); k != "" {
			keys[k] = struct{}{}
		}
	}

	rps, _ := strconv.ParseFloat(getenv("RATE_LIMIT_RPS", "100"), 64)
	burst, _ := strconv.Atoi(getenv("RATE_LIMIT_BURST", "20"))

	return GatewayConfig{
		HTTPAddr:        getenv("HTTP_ADDR", ":8080"),
		GRPCAddr:        getenv("GRPC_ADDR", ":9090"),
		JWTSecret:       []byte(getenv("JWT_SECRET", "change-me-in-production")),
		APIKeys:         keys,
		OrchestratorURL: getenv("ORCHESTRATOR_URL", "http://localhost:8081"),
		RateLimit: RateLimitConfig{
			RPS:   rps,
			Burst: burst,
		},
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
