package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("invalid token")
	ErrTokenMissing = errors.New("missing token")
)

// Scope is an API capability granted to a JWT.
type Scope string

const (
	ScopeJobsWrite   Scope = "jobs:write"
	ScopeJobsRead    Scope = "jobs:read"
	ScopeMetricsRead Scope = "metrics:read"
	ScopeConfigWrite Scope = "configs:write"
	ScopeAdminAll    Scope = "admin:all"
)

// Claims embeds jwt.RegisteredClaims with InferX-specific fields.
type Claims struct {
	TenantID string   `json:"tid"`
	Scopes   []string `json:"scp"`
	jwt.RegisteredClaims
}

// KeyManager issues and verifies JWTs, caching verified claims for 5 minutes.
type KeyManager struct {
	secret []byte
	cache  *ristretto.Cache
}

// NewKeyManager creates a KeyManager using HMAC-SHA256.
func NewKeyManager(secret string, cache *ristretto.Cache) *KeyManager {
	return &KeyManager{secret: []byte(secret), cache: cache}
}

// Issue creates a signed JWT for the given tenant with the specified scopes and expiry.
func (k *KeyManager) Issue(tenantID string, scopes []Scope, expiry time.Duration) (string, error) {
	now := time.Now()
	scopeStrs := make([]string, len(scopes))
	for i, s := range scopes {
		scopeStrs[i] = string(s)
	}
	c := Claims{
		TenantID: tenantID,
		Scopes:   scopeStrs,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(k.secret)
}

// Verify parses and validates a JWT, returning claims or a sentinel error.
// Results are cached by token hash for 5 minutes.
func (k *KeyManager) Verify(tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, ErrTokenMissing
	}

	h := sha256.Sum256([]byte(tokenString))
	cacheKey := hex.EncodeToString(h[:])
	if cached, ok := k.cache.Get(cacheKey); ok {
		if c, ok := cached.(*Claims); ok {
			return c, nil
		}
	}

	c := &Claims{}
	_, err := jwt.ParseWithClaims(tokenString, c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return k.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	if c.TenantID == "" {
		return nil, ErrTokenInvalid
	}

	k.cache.SetWithTTL(cacheKey, c, 1, 5*time.Minute)
	return c, nil
}

// HasScope reports whether claims include the required scope or ScopeAdminAll.
func (k *KeyManager) HasScope(claims *Claims, required Scope) bool {
	for _, s := range claims.Scopes {
		if Scope(s) == ScopeAdminAll || Scope(s) == required {
			return true
		}
	}
	return false
}
