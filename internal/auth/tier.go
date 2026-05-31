package auth

type Tier int

const (
	TierOSS        Tier = iota // self-hosted — no rate limits
	TierCloudFree              // 10 jobs/hr, 100 API calls/hr per tenant
	TierCloudPaid              // 100 jobs/hr, 1000 API calls/hr per tenant
	TierEnterprise             // configurable, defaults unlimited
)

// Limits holds the hourly budgets for a tenant. Zero means unlimited.
type Limits struct {
	JobsPerHour int
	APIPerHour  int
}

// DefaultLimits are the out-of-the-box limits per tier.
// Enterprise overrides are set per-tenant in config.
var DefaultLimits = map[Tier]Limits{
	TierOSS:        {0, 0},
	TierCloudFree:  {10, 100},
	TierCloudPaid:  {100, 1000},
	TierEnterprise: {0, 0},
}

func (t Tier) String() string {
	switch t {
	case TierCloudFree:
		return "cloud_free"
	case TierCloudPaid:
		return "cloud_paid"
	case TierEnterprise:
		return "enterprise"
	default:
		return "oss"
	}
}

func TierFromString(s string) Tier {
	switch s {
	case "cloud_free":
		return TierCloudFree
	case "cloud_paid":
		return TierCloudPaid
	case "enterprise":
		return TierEnterprise
	default:
		return TierOSS
	}
}
