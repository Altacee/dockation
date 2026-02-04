package master

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/artemis/docker-migrate/internal/peer"
	pb "github.com/artemis/docker-migrate/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// GRPCServer implements the MasterService
type GRPCServer struct {
	pb.UnimplementedMasterServiceServer

	master        *Master
	cryptoManager *peer.CryptoManager
	logger        *observability.Logger
	server        *grpc.Server
	proxyManager  *ProxyManager
}

// NewGRPCServer creates a new gRPC server for master
func NewGRPCServer(master *Master, cryptoManager *peer.CryptoManager, logger *observability.Logger) (*GRPCServer, error) {
	return &GRPCServer{
		master:        master,
		cryptoManager: cryptoManager,
		logger:        logger,
		proxyManager:  NewProxyManager(master.registry, logger),
	}, nil
}

// RegisterOn registers the MasterService on an existing gRPC server
func (s *GRPCServer) RegisterOn(server *grpc.Server) {
	pb.RegisterMasterServiceServer(server, s)
	pb.RegisterProxyServiceServer(server, s.proxyManager)
	s.server = server
	s.logger.Info("master service registered on existing gRPC server")
}

// Start starts a standalone gRPC server (use RegisterOn instead if another server exists)
func (s *GRPCServer) Start(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	// Get TLS credentials
	tlsConfig, err := s.cryptoManager.TLSConfig()
	if err != nil {
		return fmt.Errorf("failed to get TLS config: %w", err)
	}

	opts := []grpc.ServerOption{
		grpc.Creds(credentials.NewTLS(tlsConfig)),
	}

	s.server = grpc.NewServer(opts...)
	pb.RegisterMasterServiceServer(s.server, s)
	pb.RegisterProxyServiceServer(s.server, s.proxyManager)

	s.logger.Info("master gRPC server starting", zap.String("addr", addr))

	return s.server.Serve(lis)
}

// Stop stops the gRPC server (only if started standalone)
func (s *GRPCServer) Stop() {
	// Don't stop if registered on external server
	// The external server is responsible for shutdown
}

// RegisterWorker handles worker registration
func (s *GRPCServer) RegisterWorker(ctx context.Context, reg *pb.WorkerRegistration) (*pb.RegistrationResponse, error) {
	s.logger.Info("worker registration request",
		zap.String("name", reg.WorkerName),
		zap.String("hostname", reg.Hostname),
	)

	// Validate enrollment token
	if !s.master.ValidateEnrollmentToken(reg.EnrollmentToken) {
		s.logger.Warn("invalid enrollment token",
			zap.String("name", reg.WorkerName),
		)
		return &pb.RegistrationResponse{
			Success: false,
			Error:   "invalid enrollment token",
		}, nil
	}

	// Generate auth token for this worker
	authToken := s.master.GenerateWorkerAuthToken()

	// Register worker
	worker, err := s.master.registry.Register(reg, authToken)
	if err != nil {
		return &pb.RegistrationResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	masterCfg := s.master.GetConfig().Master

	return &pb.RegistrationResponse{
		Success:             true,
		WorkerId:            worker.ID,
		AuthToken:           authToken,
		HeartbeatIntervalMs: int64(masterCfg.HeartbeatInterval.Milliseconds()),
		InventoryIntervalMs: int64(masterCfg.InventoryInterval.Milliseconds()),
	}, nil
}

// WorkerStream handles the bidirectional stream
func (s *GRPCServer) WorkerStream(stream pb.MasterService_WorkerStreamServer) error {
	var workerID string

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			s.logger.Info("worker stream closed", zap.String("worker_id", workerID))
			return nil
		}
		if err != nil {
			s.logger.Error("worker stream error", zap.Error(err), zap.String("worker_id", workerID))
			return err
		}

		// Authenticate
		worker, ok := s.master.registry.GetByAuthToken(msg.AuthToken)
		if !ok {
			s.logger.Warn("invalid auth token on stream")
			continue
		}

		workerID = worker.ID

		// Set stream if not set
		if worker.stream == nil {
			s.master.registry.SetStream(workerID, stream)
			s.logger.Info("worker stream connected", zap.String("worker_id", workerID))
		}

		// Handle message based on payload type
		switch payload := msg.Payload.(type) {
		case *pb.WorkerMessage_Heartbeat:
			s.handleHeartbeat(workerID, payload.Heartbeat, stream)

		case *pb.WorkerMessage_MigrationProgress:
			s.master.orchestrator.UpdateProgress(payload.MigrationProgress.MigrationId, payload.MigrationProgress)

		case *pb.WorkerMessage_MigrationComplete:
			s.master.orchestrator.CompleteMigration(payload.MigrationComplete.MigrationId, payload.MigrationComplete)

		case *pb.WorkerMessage_WorkerError:
			s.logger.Error("worker error",
				zap.String("worker_id", workerID),
				zap.String("code", payload.WorkerError.ErrorCode),
				zap.String("message", payload.WorkerError.Message),
			)
		}
	}
}

func (s *GRPCServer) handleHeartbeat(workerID string, hb *pb.Heartbeat, stream pb.MasterService_WorkerStreamServer) {
	s.master.registry.UpdateHeartbeat(workerID, hb.Status, hb.SystemResources)

	// Send ack
	ack := &pb.MasterCommand{
		Payload: &pb.MasterCommand_HeartbeatAck{
			HeartbeatAck: &pb.HeartbeatAck{
				Timestamp: hb.Timestamp,
				Healthy:   true,
			},
		},
	}
	if err := stream.Send(ack); err != nil {
		s.logger.Warn("failed to send heartbeat ack",
			zap.String("worker_id", workerID),
			zap.Error(err),
		)
	}
}

// ReportResources handles resource inventory reports
func (s *GRPCServer) ReportResources(ctx context.Context, inv *pb.ResourceInventory) (*pb.AckResponse, error) {
	worker, ok := s.master.registry.GetByAuthToken(inv.AuthToken)
	if !ok {
		return &pb.AckResponse{
			Success: false,
			Error:   "invalid auth token",
		}, nil
	}

	s.master.registry.UpdateInventory(worker.ID, inv)

	s.logger.Debug("inventory updated",
		zap.String("worker_id", worker.ID),
		zap.Int("containers", len(inv.Containers)),
		zap.Int("images", len(inv.Images)),
		zap.Int("volumes", len(inv.Volumes)),
	)

	return &pb.AckResponse{Success: true}, nil
}
