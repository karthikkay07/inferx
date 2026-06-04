package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/inferbolthq/inferbolt/internal/config"
	"github.com/inferbolthq/inferbolt/internal/gateway/handler"
	mw "github.com/inferbolthq/inferbolt/internal/gateway/middleware"
)

// stubClient satisfies handler.OrchestratorClient until the orchestrator layer is built.
type stubClient struct{}

func (stubClient) Ping(_ context.Context) error { return nil }
func (stubClient) SubmitJob(_ context.Context, req handler.SubmitJobRequest) (handler.Job, error) {
	return handler.Job{ID: "stub-001", Status: "pending", Model: req.Model, Engine: req.Engine}, nil
}
func (stubClient) GetJob(_ context.Context, id string) (handler.Job, error) {
	return handler.Job{ID: id, Status: "pending"}, nil
}
func (stubClient) ListJobs(_ context.Context) ([]handler.Job, error) { return []handler.Job{}, nil }
func (stubClient) CancelJob(_ context.Context, _ string) error       { return nil }

type Server struct {
	http *http.Server
	grpc *grpcServer
}

func New(cfg config.GatewayConfig) *Server {
	ipLimiter := mw.NewIPLimiter(cfg.RateLimit.IPRequestsPerSec, cfg.RateLimit.IPBurst)
	tenantLimiter := mw.NewTenantLimiter(cfg.EnterpriseLimits)
	jobs := handler.NewJobHandler(stubClient{})

	return &Server{
		http: &http.Server{
			Addr:         cfg.HTTPAddr,
			Handler:      newRouter(cfg, jobs, ipLimiter, tenantLimiter),
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		grpc: newGRPCServer(cfg, jobs),
	}
}

// Start runs HTTP and gRPC concurrently, blocking until ctx is cancelled or either fails.
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 2)

	go func() {
		slog.Info("HTTP listening", "addr", s.http.Addr)
		if err := s.http.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	go func() {
		if err := s.grpc.start(); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errCh:
		return err
	}
}

func (s *Server) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.grpc.stop()
	return s.http.Shutdown(ctx)
}
