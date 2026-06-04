package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/inferbolthq/inferbolt/internal/auth"
)

// APIKeyEntry binds an API key to a tenant and tier.
type APIKeyEntry struct {
	TenantID string
	Tier     auth.Tier
}

type GatewayConfig struct {
	HTTPAddr         string
	GRPCAddr         string
	JWTSecret        []byte
	APIKeyStore      map[string]APIKeyEntry // key → tenant metadata
	EnterpriseLimits map[string]auth.Limits // tenantID → custom limits (enterprise only)
	RateLimit        RateLimitConfig
	OrchestratorURL  string
}

type RateLimitConfig struct {
	// Global IP-based guard applied before auth (protects public endpoints too).
	IPRequestsPerSec float64
	IPBurst          int
}

// LoadGateway reads config from environment variables.
//
// API_KEYS format:   key:tenantID:tier,key2:tenantID2:tier2
//   tier values:     oss | cloud_free | cloud_paid | enterprise
//   short form:      key  (defaults to tenantID=key, tier=oss)
//
// ENTERPRISE_LIMITS: tenantID:jobsPerHour:apiPerHour,...  (0 = unlimited)
func LoadGateway() GatewayConfig {
	keyStore := map[string]APIKeyEntry{}
	for _, raw := range strings.Split(getenv("API_KEYS", ""), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.SplitN(raw, ":", 3)
		key := parts[0]
		tenantID := key
		tier := auth.TierOSS
		if len(parts) >= 2 {
			tenantID = parts[1]
		}
		if len(parts) >= 3 {
			tier = auth.TierFromString(parts[2])
		}
		keyStore[key] = APIKeyEntry{TenantID: tenantID, Tier: tier}
	}

	enterpriseLimits := map[string]auth.Limits{}
	for _, raw := range strings.Split(getenv("ENTERPRISE_LIMITS", ""), ",") {
		raw = strings.TrimSpace(raw)
		parts := strings.SplitN(raw, ":", 3)
		if len(parts) != 3 || parts[0] == "" {
			continue
		}
		jobs, _ := strconv.Atoi(parts[1])
		api, _ := strconv.Atoi(parts[2])
		enterpriseLimits[parts[0]] = auth.Limits{JobsPerHour: jobs, APIPerHour: api}
	}

	rps, _ := strconv.ParseFloat(getenv("IP_RATE_LIMIT_RPS", "50"), 64)
	burst, _ := strconv.Atoi(getenv("IP_RATE_LIMIT_BURST", "10"))

	return GatewayConfig{
		HTTPAddr:         getenv("HTTP_ADDR", ":8080"),
		GRPCAddr:         getenv("GRPC_ADDR", ":9090"),
		JWTSecret:        []byte(getenv("JWT_SECRET", "change-me-in-production")),
		APIKeyStore:      keyStore,
		EnterpriseLimits: enterpriseLimits,
		OrchestratorURL:  getenv("ORCHESTRATOR_URL", "http://localhost:8081"),
		RateLimit: RateLimitConfig{
			IPRequestsPerSec: rps,
			IPBurst:          burst,
		},
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
