package gateway_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iauth "github.com/inferbolthq/inferbolt/internal/auth"
)

// ── shared test helpers ───────────────────────────────────────────────────────

func newCache(t *testing.T) *ristretto.Cache {
	t.Helper()
	c, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e5,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

func newKM(t *testing.T) *iauth.KeyManager {
	t.Helper()
	return iauth.NewKeyManager("test-secret-must-be-32-chars-long!!", newCache(t))
}

func okHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

// ── AuthMiddleware ─────────────────────────────────────────────────────────────

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	km := newKM(t)
	h := iauth.AuthMiddleware(km)(http.HandlerFunc(okHandler))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "missing authorization header")
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	km := newKM(t)
	h := iauth.AuthMiddleware(km)(http.HandlerFunc(okHandler))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer not.a.valid.token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid token")
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	km := newKM(t)
	token, err := km.Issue("t1", []iauth.Scope{iauth.ScopeJobsRead}, -time.Hour)
	require.NoError(t, err)

	h := iauth.AuthMiddleware(km)(http.HandlerFunc(okHandler))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "token expired")
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	km := newKM(t)
	token, err := km.Issue("t1", []iauth.Scope{iauth.ScopeJobsRead}, time.Hour)
	require.NoError(t, err)

	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = iauth.GetTenantID(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := iauth.AuthMiddleware(km)(next)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "t1", got)
}

// ── RateLimitMiddleware ────────────────────────────────────────────────────────

func TestRateLimitMiddleware_UnderLimit(t *testing.T) {
	h := iauth.RateLimitMiddleware(newCache(t))(http.HandlerFunc(okHandler))

	for i := 0; i < 50; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r = r.WithContext(iauth.SetTenantID(r.Context(), "rl-under"))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code, "request %d should succeed", i+1)
		remaining, err := strconv.Atoi(w.Header().Get("X-RateLimit-Remaining"))
		require.NoError(t, err)
		assert.Equal(t, 99-i, remaining, "remaining mismatch on request %d", i+1)
	}
}

func TestRateLimitMiddleware_OverLimit(t *testing.T) {
	h := iauth.RateLimitMiddleware(newCache(t))(http.HandlerFunc(okHandler))
	const tenant = "rl-over"

	for i := 0; i < 101; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r = r.WithContext(iauth.SetTenantID(r.Context(), tenant))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if i < 100 {
			assert.Equal(t, http.StatusOK, w.Code, "request %d should succeed", i+1)
		} else {
			assert.Equal(t, http.StatusTooManyRequests, w.Code, "request 101 should be rate limited")
			assert.Contains(t, w.Body.String(), "rate limit exceeded")
		}
	}
}

// ── RequireScope ──────────────────────────────────────────────────────────────

func TestRequireScope_HasScope(t *testing.T) {
	km := newKM(t)
	token, err := km.Issue("t1", []iauth.Scope{iauth.ScopeJobsWrite}, time.Hour)
	require.NoError(t, err)

	var reached bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	h := iauth.AuthMiddleware(km)(iauth.RequireScope(km, iauth.ScopeJobsWrite)(next))

	r := httptest.NewRequest(http.MethodPost, "/v1/jobs", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, reached)
}

func TestRequireScope_MissingScope(t *testing.T) {
	km := newKM(t)
	token, err := km.Issue("t1", []iauth.Scope{iauth.ScopeJobsRead}, time.Hour)
	require.NoError(t, err)

	h := iauth.AuthMiddleware(km)(iauth.RequireScope(km, iauth.ScopeJobsWrite)(http.HandlerFunc(okHandler)))

	r := httptest.NewRequest(http.MethodPost, "/v1/jobs", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "insufficient scope")
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}
