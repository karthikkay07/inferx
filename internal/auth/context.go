package auth

import "context"

type contextKey string

const (
	tenantIDKey contextKey = "tenant_id"
	tierKey     contextKey = "tier"
)

func SetTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

// GetTenantID returns the tenant ID and whether it was present in the context.
func GetTenantID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(tenantIDKey).(string)
	return id, ok && id != ""
}

// MustGetTenantID panics if tenant_id is not in the context.
// Use only in code paths that are always downstream of the auth middleware.
func MustGetTenantID(ctx context.Context) string {
	id, ok := GetTenantID(ctx)
	if !ok {
		panic("tenant_id not in context")
	}
	return id
}

func SetTier(ctx context.Context, t Tier) context.Context {
	return context.WithValue(ctx, tierKey, t)
}

func GetTier(ctx context.Context) Tier {
	t, _ := ctx.Value(tierKey).(Tier)
	return t
}
