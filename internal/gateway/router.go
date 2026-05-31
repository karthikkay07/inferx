package gateway

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/karthikkay07/inferx/internal/config"
	"github.com/karthikkay07/inferx/internal/gateway/handler"
	mw "github.com/karthikkay07/inferx/internal/gateway/middleware"
)

func newRouter(cfg config.GatewayConfig, jobs *handler.JobHandler, rl *mw.Limiter) http.Handler {
	r := chi.NewRouter()

	// Global middleware — order is significant
	r.Use(mw.Recovery)
	r.Use(mw.Logger)
	r.Use(rl.Middleware)

	// Public — no auth
	r.Get("/health", handler.Health)
	r.Get("/ready", handler.Ready(jobs))

	// Protected — auth required
	r.Group(func(r chi.Router) {
		r.Use(mw.Auth(cfg.APIKeys, cfg.JWTSecret))

		r.Route("/v1", func(r chi.Router) {
			r.Post("/jobs", jobs.Submit)
			r.Get("/jobs", jobs.List)
			r.Get("/jobs/{id}", jobs.Get)
			r.Delete("/jobs/{id}", jobs.Cancel)
		})
	})

	return r
}
