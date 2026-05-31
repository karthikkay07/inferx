package gateway

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	iauth "github.com/karthikkay07/inferx/internal/auth"
	"github.com/karthikkay07/inferx/internal/config"
	"github.com/karthikkay07/inferx/internal/gateway/handler"
	mw "github.com/karthikkay07/inferx/internal/gateway/middleware"
)

func newRouter(
	cfg config.GatewayConfig,
	jobs *handler.JobHandler,
	ipLimiter *mw.IPLimiter,
	tenantLimiter *mw.TenantLimiter,
) http.Handler {
	r := chi.NewRouter()

	// Global — runs on every request including /health
	r.Use(mw.Recovery)
	r.Use(mw.Logger)
	r.Use(ipLimiter.Middleware) // coarse IP guard before tenant context exists

	// Public — no auth, no tenant limits
	r.Get("/health", handler.Health)
	r.Get("/ready", handler.Ready(jobs))

	// Protected — auth sets tenant_id + tier in context, then tier limits apply
	r.Group(func(r chi.Router) {
		r.Use(mw.Auth(keyLookupFrom(cfg), cfg.JWTSecret))
		r.Use(tenantLimiter.MiddlewareAPI) // counts every /v1/* call against API budget

		r.Route("/v1", func(r chi.Router) {
			// POST /jobs also burns from the jobs/hr budget
			r.Post("/jobs", tenantLimiter.MiddlewareJob(http.HandlerFunc(jobs.Submit)))
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
