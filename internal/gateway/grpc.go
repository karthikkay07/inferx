package gateway

import (
	"context"
	"log/slog"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	iauth "github.com/karthikkay07/inferx/internal/auth"
	"github.com/karthikkay07/inferx/internal/config"
	"github.com/karthikkay07/inferx/internal/gateway/handler"
)

type grpcServer struct {
	server *grpc.Server
	addr   string
}

func newGRPCServer(cfg config.GatewayConfig, jobs *handler.JobHandler) *grpcServer {
	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpcRecovery(),
			grpcLogger(),
			grpcAuth(cfg),
		),
	)

	// Enables grpcurl introspection without distributing .proto files.
	reflection.Register(s)

	// TODO: register generated service after running `make proto`
	// inferxv1.RegisterGatewayServer(s, newGRPCJobsServer(jobs))
	_ = jobs

	return &grpcServer{server: s, addr: cfg.GRPCAddr}
}

func (g *grpcServer) start() error {
	lis, err := net.Listen("tcp", g.addr)
	if err != nil {
		return err
	}
	slog.Info("gRPC listening", "addr", g.addr)
	return g.server.Serve(lis)
}

func (g *grpcServer) stop() {
	g.server.GracefulStop()
}

// ---- unary interceptors ----

func grpcRecovery() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("gRPC panic", "error", r)
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()
		return h(ctx, req)
	}
}

func grpcLogger() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		resp, err := h(ctx, req)
		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}
		tenantID := iauth.GetTenantID(ctx)
		slog.Info("grpc", "method", info.FullMethod, "code", code.String(), "tenant_id", tenantID)
		return resp, err
	}
}

// grpcAuth validates the API key from gRPC metadata and sets tenant context.
func grpcAuth(cfg config.GatewayConfig) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		if vals := md.Get("x-api-key"); len(vals) > 0 {
			entry, found := cfg.APIKeyStore[vals[0]]
			if !found {
				return nil, status.Error(codes.Unauthenticated, "invalid api key")
			}
			ctx = iauth.SetTenantID(ctx, entry.TenantID)
			ctx = iauth.SetTier(ctx, entry.Tier)
			return h(ctx, req)
		}

		if vals := md.Get("authorization"); len(vals) > 0 {
			bearer := vals[0]
			if strings.HasPrefix(bearer, "Bearer ") {
				// TODO: wire validateJWT from middleware/auth.go
				_ = strings.TrimPrefix(bearer, "Bearer ")
			}
		}

		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}
}
