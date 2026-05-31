package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type ctxKey string

const CtxKeyClaims ctxKey = "claims"

type Claims struct {
	jwt.RegisteredClaims
	KeyID  string   `json:"kid"`
	Scopes []string `json:"scopes"`
}

// Auth validates X-API-Key or Authorization: Bearer <jwt>.
// Attach this only to protected route groups — /health and /ready bypass it.
func Auth(keys map[string]struct{}, jwtSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var (
				claims *Claims
				err    error
			)

			switch {
			case r.Header.Get("X-API-Key") != "":
				claims, err = validateAPIKey(r.Header.Get("X-API-Key"), keys)
			case strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "):
				token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				claims, err = validateJWT(token, jwtSecret)
			default:
				writeUnauthorized(w, "missing credentials")
				return
			}

			if err != nil {
				writeUnauthorized(w, "invalid credentials")
				return
			}

			ctx := context.WithValue(r.Context(), CtxKeyClaims, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ClaimsFromCtx(ctx context.Context) *Claims {
	c, _ := ctx.Value(CtxKeyClaims).(*Claims)
	return c
}

func validateAPIKey(key string, keys map[string]struct{}) (*Claims, error) {
	if _, ok := keys[key]; !ok {
		return nil, errors.New("unknown api key")
	}
	return &Claims{KeyID: key, Scopes: []string{"*"}}, nil
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
