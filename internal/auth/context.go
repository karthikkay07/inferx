package auth

import "context"

// contextKey is unexported so no other package can collide with these keys.
type contextKey string

const (
	keyTenantID contextKey = "tenant_id"
	keyTier     contextKey = "tier"
)

func SetTenantID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyTenantID, id)
}

func GetTenantID(ctx context.Context) string {
	id, _ := ctx.Value(keyTenantID).(string)
	return id
}

func SetTier(ctx context.Context, t Tier) context.Context {
	return context.WithValue(ctx, keyTier, t)
}

func GetTier(ctx context.Context) Tier {
	t, _ := ctx.Value(keyTier).(Tier)
	return t
}
