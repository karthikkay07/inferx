package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	iauth "github.com/inferbolthq/inferbolt/internal/auth"
)

// ---- IP-based global limiter ------------------------------------------------
// Applied before auth to protect all routes (including /health) from flooding.

type IPLimiter struct {
	mu      sync.Mutex
	clients map[string]*ipState
	rate    rate.Limit
	burst   int
}

type ipState struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewIPLimiter(rps float64, burst int) *IPLimiter {
	l := &IPLimiter{
		clients: make(map[string]*ipState),
		rate:    rate.Limit(rps),
		burst:   burst,
	}
	go l.cleanup()
	return l
}

func (l *IPLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := realIP(r)
		if !l.allow(ip) {
			rateLimitError(w, "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *IPLimiter) allow(ip string) bool {
	l.mu.Lock()
	s, ok := l.clients[ip]
	if !ok {
		s = &ipState{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.clients[ip] = s
	}
	s.lastSeen = time.Now()
	l.mu.Unlock()
	return s.limiter.Allow()
}

func (l *IPLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		for k, s := range l.clients {
			if time.Since(s.lastSeen) > 5*time.Minute {
				delete(l.clients, k)
			}
		}
		l.mu.Unlock()
	}
}

// ---- Tenant-aware tier limiter ----------------------------------------------
// Applied inside the auth group. Tracks API calls/hr and job submissions/hr
// separately, per tenant, according to their tier's default limits
// (or enterprise overrides).

type TenantLimiter struct {
	mu        sync.Mutex
	tenants   map[string]*tenantBucket
	overrides map[string]iauth.Limits // tenantID → custom limits
}

type tenantBucket struct {
	tier     iauth.Tier
	api      *rate.Limiter // nil = unlimited
	jobs     *rate.Limiter // nil = unlimited
	lastSeen time.Time
}

func NewTenantLimiter(overrides map[string]iauth.Limits) *TenantLimiter {
	tl := &TenantLimiter{
		tenants:   make(map[string]*tenantBucket),
		overrides: overrides,
	}
	go tl.cleanup()
	return tl
}

// MiddlewareAPI checks the API calls/hr budget for every protected request.
func (tl *TenantLimiter) MiddlewareAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := iauth.GetTenantID(r.Context())
		if !ok {
			next.ServeHTTP(w, r) // no tenant context — shouldn't happen on protected routes
			return
		}
		b := tl.bucket(tenantID, iauth.GetTier(r.Context()))
		if b.api != nil && !b.api.Allow() {
			rateLimitError(w, "API call rate limit exceeded — upgrade your plan or retry next hour")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// MiddlewareJob additionally checks the jobs/hr budget. Wrap POST /v1/jobs with this.
func (tl *TenantLimiter) MiddlewareJob(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := iauth.GetTenantID(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		b := tl.bucket(tenantID, iauth.GetTier(r.Context()))
		if b.jobs != nil && !b.jobs.Allow() {
			rateLimitError(w, "job submission rate limit exceeded — upgrade your plan or retry next hour")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (tl *TenantLimiter) bucket(tenantID string, tier iauth.Tier) *tenantBucket {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	b, ok := tl.tenants[tenantID]
	// Recreate bucket if tier changed (e.g. plan upgrade takes effect immediately).
	if !ok || b.tier != tier {
		limits := tl.limitsFor(tenantID, tier)
		b = &tenantBucket{
			tier: tier,
			api:  hourlyLimiter(limits.APIPerHour),
			jobs: hourlyLimiter(limits.JobsPerHour),
		}
		tl.tenants[tenantID] = b
	}
	b.lastSeen = time.Now()
	return b
}

func (tl *TenantLimiter) limitsFor(tenantID string, tier iauth.Tier) iauth.Limits {
	if override, ok := tl.overrides[tenantID]; ok {
		return override
	}
	return iauth.DefaultLimits[tier]
}

func (tl *TenantLimiter) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		tl.mu.Lock()
		for k, b := range tl.tenants {
			if time.Since(b.lastSeen) > time.Hour {
				delete(tl.tenants, k)
			}
		}
		tl.mu.Unlock()
	}
}

// hourlyLimiter creates a token-bucket with a per-hour rate and burst equal to
// the full hourly allowance (lets a tenant "save up" and burst, same as most APIs).
// Returns nil for unlimited (perHour == 0).
func hourlyLimiter(perHour int) *rate.Limiter {
	if perHour == 0 {
		return nil
	}
	r := rate.Limit(float64(perHour) / 3600.0)
	return rate.NewLimiter(r, perHour)
}

// ---- shared helpers ---------------------------------------------------------

func rateLimitError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "3600")
	w.WriteHeader(http.StatusTooManyRequests)
	w.Write([]byte(`{"error":"` + msg + `"}`)) //nolint:errcheck
}

func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.SplitN(ip, ",", 2)[0]
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if i := strings.LastIndex(r.RemoteAddr, ":"); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}
