package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	iauth "github.com/inferbolthq/inferbolt/internal/auth"
)

type ctxKey string

const CtxKeyClaims ctxKey = "claims"

// Claims is stored in context after successful authentication.
type Claims struct {
	jwt.RegisteredClaims
	TenantID string   `json:"tenant_id"`
	TierStr  string   `json:"tier"` // JSON field stays "tier" for JWT interop
	Scopes   []string `json:"scopes"`
}

// KeyLookupFunc resolves an API key to its tenant ID and tier.
// Decouples middleware from the config package.
type KeyLookupFunc func(key string) (tenantID string, tier iauth.Tier, ok bool)

// Auth validates X-API-Key or Authorization: Bearer <jwt>.
// On success it sets TenantID and Tier into context via internal/auth helpers.
// Attach only to protected route groups — /health and /ready bypass it.
func Auth(lookup KeyLookupFunc, jwtSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var claims *Claims

			switch {
			case r.Header.Get("X-API-Key") != "":
				tenantID, tier, ok := lookup(r.Header.Get("X-API-Key"))
				if !ok {
					writeUnauthorized(w, "invalid api key")
					return
				}
				claims = &Claims{
					TenantID: tenantID,
					TierStr:  tier.String(),
					Scopes:   []string{"*"},
				}

			case strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "):
				token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				var err error
				claims, err = validateJWT(token, jwtSecret)
				if err != nil {
					writeUnauthorized(w, "invalid token")
					return
				}

			default:
				writeUnauthorized(w, "missing credentials")
				return
			}

			// Propagate tenant identity everywhere downstream via canonical helpers.
			ctx := context.WithValue(r.Context(), CtxKeyClaims, claims)
			ctx = iauth.SetTenantID(ctx, claims.TenantID)
			ctx = iauth.SetTier(ctx, iauth.TierFromString(claims.TierStr))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ClaimsFromCtx(ctx context.Context) *Claims {
	c, _ := ctx.Value(CtxKeyClaims).(*Claims)
	return c
}

func validateJWT(tokenStr string, secret []byte) (*Claims, error) {
	c := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + msg + `"}`)) //nolint:errcheck
}
