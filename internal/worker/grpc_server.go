package worker

import (
	"context"
	"fmt"
	"net"

	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/artemis/docker-migrate/internal/peer"
	pb "github.com/artemis/docker-migrate/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// GRPCServer implements WorkerService
type GRPCServer struct {
	pb.UnimplementedWorkerServiceServer

	worker        *Worker
	cryptoManager *peer.CryptoManager
	logger        *observability.Logger
	server        *grpc.Server
}

// NewGRPCServer creates a new gRPC server
func NewGRPCServer(worker *Worker, cryptoManager *peer.CryptoManager, logger *observability.Logger) (*GRPCServer, error) {
	return &GRPCServer{
		worker:        worker,
		cryptoManager: cryptoManager,
		logger:        logger,
	}, nil
}

// Start starts the gRPC server
func (s *GRPCServer) Start(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	tlsConfig, err := s.cryptoManager.GetServerTLSConfig()
	if err != nil {
		return fmt.Errorf("failed to get TLS config: %w", err)
	}

	opts := []grpc.ServerOption{
		grpc.Creds(credentials.NewTLS(tlsConfig)),
	}

	s.server = grpc.NewServer(opts...)
	pb.RegisterWorkerServiceServer(s.server, s)

	s.logger.Info("worker gRPC server starting", zap.String("addr", addr))

	return s.server.Serve(lis)
}

// Stop stops the server
func (s *GRPCServer) Stop() {
	if s.server != nil {
		s.server.GracefulStop()
	}
}

// InitiateMigration handles migration initiation request
func (s *GRPCServer) InitiateMigration(ctx context.Context, req *pb.MigrationRequest) (*pb.MigrationResponse, error) {
	s.logger.Info("migration initiation request",
		zap.String("migration_id", req.MigrationId),
		zap.String("target", req.TargetAddress),
	)

	// This is called directly by master if stream isn't available
	// For now, acknowledge and let the stream-based command handle it
	return &pb.MigrationResponse{
		Accepted:    true,
		MigrationId: req.MigrationId,
	}, nil
}

// AcceptMigration handles migration acceptance request
func (s *GRPCServer) AcceptMigration(ctx context.Context, req *pb.AcceptMigrationRequest) (*pb.AcceptMigrationResponse, error) {
	s.logger.Info("migration acceptance request",
		zap.String("migration_id", req.MigrationId),
		zap.String("source", req.SourceAddress),
	)

	return &pb.AcceptMigrationResponse{
		Accepted:       true,
		ReceiveAddress: s.worker.GetConfig().GRPCAddr,
	}, nil
}

// HealthCheck returns worker health status
func (s *GRPCServer) HealthCheck(ctx context.Context, _ *pb.Empty) (*pb.HealthResponse, error) {
	checks := make(map[string]string)

	// Check Docker connection
	if err := s.worker.docker.Ping(ctx); err != nil {
		checks["docker"] = "unhealthy: " + err.Error()
	} else {
		checks["docker"] = "healthy"
	}

	// Check master connection
	if s.worker.connector != nil && s.worker.connector.IsConnected() {
		checks["master"] = "connected"
	} else {
		checks["master"] = "disconnected"
	}

	return &pb.HealthResponse{
		Healthy:       true,
		Status:        pb.WorkerStatus_WORKER_STATUS_IDLE,
		Version:       "1.0.0",
		UptimeSeconds: int64(s.worker.GetUptime().Seconds()),
		Checks:        checks,
	}, nil
}

// CancelMigration cancels an active migration
func (s *GRPCServer) CancelMigration(ctx context.Context, req *pb.CancelMigrationRequest) (*pb.CancelMigrationResponse, error) {
	s.logger.Info("cancel migration request",
		zap.String("migration_id", req.MigrationId),
		zap.String("reason", req.Reason),
	)

	s.worker.executor.Cancel(req.MigrationId)

	return &pb.CancelMigrationResponse{
		Success: true,
	}, nil
}
