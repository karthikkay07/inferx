package gateway

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	iauth "github.com/inferbolthq/inferbolt/internal/auth"
	"github.com/inferbolthq/inferbolt/internal/config"
	"github.com/inferbolthq/inferbolt/internal/gateway/handler"
	mw "github.com/inferbolthq/inferbolt/internal/gateway/middleware"
)

func newRouter(
	cfg config.GatewayConfig,
	jobs *handler.JobHandler,
	ipLimiter *mw.IPLimiter,
	tenantLimiter *mw.TenantLimiter,
) http.Handler {
	r := chi.NewRouter()

	// ── Global middleware (all routes, in order) ────────────────────────────
	r.Use(mw.Recovery)                   // catch panics before anything else
	r.Use(mw.RequestID)                  // inject/propagate X-Request-ID
	r.Use(mw.Logger)                     // structured log: method, path, status, duration, request_id, trace_id
	r.Use(mw.OTelTrace)                  // start server span, propagate W3C TraceContext
	r.Use(ipLimiter.Middleware)          // coarse IP guard (pre-auth, covers public routes)
	r.Use(mw.Timeout(30 * time.Second)) // hard deadline — must come before any I/O

	// ── Public routes (no auth) ─────────────────────────────────────────────
	r.Get("/health", handler.Health)
	r.Get("/ready", handler.Ready(jobs))

	// ── Protected routes ────────────────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(mw.Auth(keyLookupFrom(cfg), cfg.JWTSecret)) // sets tenant_id + tier in ctx
		r.Use(tenantLimiter.MiddlewareAPI)                 // checks API calls/hr budget

		r.Route("/v1", func(r chi.Router) {
			// POST /jobs also burns from the jobs/hr budget
			r.Post("/jobs", tenantLimiter.MiddlewareJob(http.HandlerFunc(jobs.Submit)).ServeHTTP)
			r.Get("/jobs", jobs.List)
			r.Get("/jobs/{id}", jobs.Get)
			r.Delete("/jobs/{id}", jobs.Cancel)
		})
	})

	return r
}

func keyLookupFrom(cfg config.GatewayConfig) mw.KeyLookupFunc {
	return func(key string) (string, iauth.Tier, bool) {
		entry, ok := cfg.APIKeyStore[key]
		if !ok {
			return "", 0, false
		}
		return entry.TenantID, entry.Tier, true
	}
}
