package peer

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/artemis/docker-migrate/internal/config"
	"github.com/artemis/docker-migrate/internal/docker"
	"github.com/artemis/docker-migrate/internal/observability"
	pb "github.com/artemis/docker-migrate/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	// gRPC keepalive parameters
	KeepaliveTime    = 30 * time.Second
	KeepaliveTimeout = 10 * time.Second
	MaxConnectionAge = 24 * time.Hour
)

// GRPCServer handles gRPC server for peer communication
type GRPCServer struct {
	pb.UnimplementedMigrationServiceServer
	server     *grpc.Server
	docker     *docker.Client
	transfer   *TransferManager
	pairing    *PairingManager
	crypto     *CryptoManager
	config     *config.Config
	logger     *observability.Logger
	peerID     string
}

// NewGRPCServer creates a new gRPC server
func NewGRPCServer(
	dockerClient *docker.Client,
	transfer *TransferManager,
	pairing *PairingManager,
	crypto *CryptoManager,
	cfg *config.Config,
	logger *observability.Logger,
) (*GRPCServer, error) {

	peerID := fmt.Sprintf("peer-%s", crypto.GetFingerprint()[:8])

	gs := &GRPCServer{
		docker:   dockerClient,
		transfer: transfer,
		pairing:  pairing,
		crypto:   crypto,
		config:   cfg,
		logger:   logger,
		peerID:   peerID,
	}

	// Get TLS config
	tlsConfig, err := crypto.TLSConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get TLS config: %w", err)
	}

	creds := credentials.NewTLS(tlsConfig)

	// Create gRPC server with security and keepalive
	gs.server = grpc.NewServer(
		grpc.Creds(creds),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    KeepaliveTime,
			Timeout: KeepaliveTimeout,
			MaxConnectionAge: MaxConnectionAge,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             15 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.MaxRecvMsgSize(8 * 1024 * 1024), // 8MB max message size
		grpc.MaxSendMsgSize(8 * 1024 * 1024),
		grpc.UnaryInterceptor(gs.unaryInterceptor),
		grpc.StreamInterceptor(gs.streamInterceptor),
	)

	pb.RegisterMigrationServiceServer(gs.server, gs)

	return gs, nil
}

// Start starts the gRPC server
func (gs *GRPCServer) Start(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	gs.logger.Info("starting gRPC server", zap.String("addr", addr))

	if err := gs.server.Serve(listener); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

// Stop stops the gRPC server gracefully
func (gs *GRPCServer) Stop() {
	if gs.server != nil {
		gs.logger.Info("stopping gRPC server")
		gs.server.GracefulStop()
	}
}

// TransferVolume implements streaming volume transfer
func (gs *GRPCServer) TransferVolume(stream pb.MigrationService_TransferVolumeServer) error {
	ctx := stream.Context()

	// Get peer info
	peerInfo, ok := peer.FromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "no peer info")
	}

	gs.logger.Info("volume transfer started",
		zap.String("peer_addr", peerInfo.Addr.String()),
	)

	var volumeID string
	var totalSize int64
	var writer *ChunkWriter
	var tmpFile *os.File
	receivedBytes := int64(0)
	startTime := time.Now()

	// Create temporary file for atomic write
	tmpFile, err := os.CreateTemp("", "volume-transfer-*")
	if err != nil {
		return status.Errorf(codes.Internal, "failed to create temp file: %v", err)
	}
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}()

	writer = NewChunkWriter(tmpFile, 0, gs.logger)

	// Receive chunks
	for {
		select {
		case <-ctx.Done():
			return status.Error(codes.Canceled, "transfer canceled")
		default:
		}

		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			gs.logger.Error("failed to receive chunk", zap.Error(err))
			return status.Errorf(codes.Internal, "receive error: %v", err)
		}

		// First chunk initializes transfer
		if volumeID == "" {
			volumeID = chunk.VolumeId
			totalSize = chunk.TotalSize
			gs.logger.Info("receiving volume",
				zap.String("volume_id", volumeID),
				zap.Int64("total_size", totalSize),
			)
		}

		// Write chunk with verification
		peerChunk := &Chunk{
			Offset:   chunk.Offset,
			Data:     chunk.Data,
			Checksum: chunk.Checksum,
			Size:     len(chunk.Data),
			IsFinal:  chunk.IsFinal,
		}

		if err := writer.WriteChunk(peerChunk); err != nil {
			gs.logger.Error("failed to write chunk",
				zap.Int64("offset", chunk.Offset),
				zap.Error(err),
			)
			// Send error ack
			stream.Send(&pb.TransferAck{
				Offset:   chunk.Offset,
				Success:  false,
				Error:    err.Error(),
				Progress: float32(receivedBytes) / float32(totalSize),
			})
			return status.Errorf(codes.DataLoss, "write error: %v", err)
		}

		receivedBytes += int64(len(chunk.Data))

		// Send success ack
		progress := float32(receivedBytes) / float32(totalSize)
		if err := stream.Send(&pb.TransferAck{
			Offset:   chunk.Offset + int64(len(chunk.Data)),
			Success:  true,
			Progress: progress,
		}); err != nil {
			gs.logger.Error("failed to send ack", zap.Error(err))
			return status.Errorf(codes.Internal, "ack error: %v", err)
		}

		// Log progress
		if receivedBytes%(100*1024*1024) == 0 { // Every 100MB
			gs.logger.Info("transfer progress",
				zap.String("volume_id", volumeID),
				zap.Float32("progress", progress*100),
				zap.Int64("received_bytes", receivedBytes),
			)
		}

		if chunk.IsFinal {
			break
		}
	}

	duration := time.Since(startTime)
	speed := float64(receivedBytes) / duration.Seconds() / (1024 * 1024)

	gs.logger.Info("volume transfer completed",
		zap.String("volume_id", volumeID),
		zap.Int64("total_bytes", receivedBytes),
		zap.Duration("duration", duration),
		zap.Float64("speed_mbps", speed),
	)

	// TODO: Import volume into Docker
	// This would involve: tmpFile -> Docker volume import

	return nil
}

// Ping checks peer connectivity and latency
func (gs *GRPCServer) Ping(ctx context.Context, req *pb.Empty) (*pb.Pong, error) {
	return &pb.Pong{
		PeerId:    gs.peerID,
		Timestamp: time.Now().Unix(),
		Version:   "1.0.0",
	}, nil
}

// unaryInterceptor adds logging and authentication to unary calls
func (gs *GRPCServer) unaryInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	start := time.Now()

	// Verify peer certificate
	if err := gs.verifyPeer(ctx); err != nil {
		gs.logger.Warn("peer verification failed", zap.Error(err))
		return nil, status.Error(codes.Unauthenticated, "peer not trusted")
	}

	// Call handler
	resp, err := handler(ctx, req)

	// Log
	gs.logger.Info("unary call",
		zap.String("method", info.FullMethod),
		zap.Duration("duration", time.Since(start)),
		zap.Error(err),
	)

	return resp, err
}

// streamInterceptor adds logging and authentication to streams
func (gs *GRPCServer) streamInterceptor(
	srv interface{},
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	start := time.Now()

	// Verify peer certificate
	if err := gs.verifyPeer(ss.Context()); err != nil {
		gs.logger.Warn("peer verification failed", zap.Error(err))
		return status.Error(codes.Unauthenticated, "peer not trusted")
	}

	// Call handler
	err := handler(srv, ss)

	// Log
	gs.logger.Info("stream call",
		zap.String("method", info.FullMethod),
		zap.Duration("duration", time.Since(start)),
		zap.Error(err),
	)

	return err
}

// verifyPeer verifies the peer certificate is trusted
func (gs *GRPCServer) verifyPeer(ctx context.Context) error {
	peerInfo, ok := peer.FromContext(ctx)
	if !ok {
		return fmt.Errorf("no peer info in context")
	}

	tlsInfo, ok := peerInfo.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return fmt.Errorf("no TLS info")
	}

	if len(tlsInfo.State.PeerCertificates) == 0 {
		return fmt.Errorf("no peer certificates")
	}

	cert := tlsInfo.State.PeerCertificates[0]
	fingerprint := ComputeFingerprint(cert)

	if !gs.crypto.IsTrusted(fingerprint) {
		return fmt.Errorf("peer certificate not trusted: %s", fingerprint)
	}

	// Update last seen
	if trustedPeer, ok := gs.pairing.GetTrustedPeer(fingerprint); ok {
		gs.pairing.UpdatePeerLastSeen(trustedPeer.ID)
	}

	return nil
}

// GRPCClient handles gRPC client for peer communication
type GRPCClient struct {
	conn     *grpc.ClientConn
	client   pb.MigrationServiceClient
	transfer *TransferManager
	crypto   *CryptoManager
	logger   *observability.Logger
}

// NewGRPCClient creates a new gRPC client
func NewGRPCClient(
	address string,
	expectedFingerprint string,
	transfer *TransferManager,
	crypto *CryptoManager,
	logger *observability.Logger,
) (*GRPCClient, error) {

	// Get TLS config with fingerprint verification
	tlsConfig, err := crypto.TLSClientConfig(expectedFingerprint)
	if err != nil {
		return nil, fmt.Errorf("failed to get TLS config: %w", err)
	}

	creds := credentials.NewTLS(tlsConfig)

	// Create gRPC connection
	conn, err := grpc.Dial(address,
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                KeepaliveTime,
			Timeout:             KeepaliveTimeout,
			PermitWithoutStream: true,
		}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(8*1024*1024),
			grpc.MaxCallSendMsgSize(8*1024*1024),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	client := pb.NewMigrationServiceClient(conn)

	logger.Info("gRPC client connected",
		zap.String("address", address),
	)

	return &GRPCClient{
		conn:     conn,
		client:   client,
		transfer: transfer,
		crypto:   crypto,
		logger:   logger,
	}, nil
}

// SendVolume streams volume to peer
func (gc *GRPCClient) SendVolume(ctx context.Context, volumeID string, reader io.Reader, totalSize int64) error {
	stream, err := gc.client.TransferVolume(ctx)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}

	// Create transfer tracking
	transfer, err := gc.transfer.CreateTransfer(ctx, TransferVolume, volumeID, "peer", totalSize)
	if err != nil {
		return fmt.Errorf("failed to create transfer: %w", err)
	}

	transfer.Status = TransferActive

	// Create chunk reader with dynamic sizing
	chunkSize := gc.transfer.DynamicChunkSize(transfer)
	chunkReader := NewChunkReader(reader, chunkSize, totalSize)

	gc.logger.Info("starting volume transfer",
		zap.String("volume_id", volumeID),
		zap.Int64("total_size", totalSize),
		zap.Int("chunk_size", chunkSize),
	)

	// Send chunks
	for {
		select {
		case <-ctx.Done():
			gc.transfer.CancelTransfer(transfer.ID)
			return ctx.Err()
		default:
		}

		chunk, err := chunkReader.ReadChunk()
		if err == io.EOF {
			break
		}
		if err != nil {
			gc.transfer.FailTransfer(transfer.ID, err)
			return fmt.Errorf("failed to read chunk: %w", err)
		}

		// Send chunk
		pbChunk := &pb.VolumeChunk{
			VolumeId:  volumeID,
			Offset:    chunk.Offset,
			Data:      chunk.Data,
			Checksum:  chunk.Checksum,
			TotalSize: totalSize,
			IsFinal:   chunk.IsFinal,
		}

		if err := stream.Send(pbChunk); err != nil {
			gc.transfer.FailTransfer(transfer.ID, err)
			return fmt.Errorf("failed to send chunk: %w", err)
		}

		// Receive ack
		ack, err := stream.Recv()
		if err != nil {
			gc.transfer.FailTransfer(transfer.ID, err)
			return fmt.Errorf("failed to receive ack: %w", err)
		}

		if !ack.Success {
			err := fmt.Errorf("chunk transfer failed: %s", ack.Error)
			gc.transfer.FailTransfer(transfer.ID, err)
			return err
		}

		// Add checkpoint
		gc.transfer.AddCheckpoint(transfer.ID, chunk.Offset+int64(chunk.Size), chunk.Checksum)

		// Adjust chunk size based on performance
		if len(transfer.Checkpoints)%10 == 0 {
			newSize := gc.transfer.DynamicChunkSize(transfer)
			if newSize != chunkSize {
				chunkSize = newSize
				gc.logger.Info("adjusted chunk size",
					zap.String("transfer_id", transfer.ID),
					zap.Int("new_size", newSize),
				)
			}
		}

		if chunk.IsFinal {
			break
		}
	}

	// Close and verify
	if err := stream.CloseSend(); err != nil {
		gc.transfer.FailTransfer(transfer.ID, err)
		return fmt.Errorf("failed to close stream: %w", err)
	}

	gc.transfer.CompleteTransfer(transfer.ID)

	gc.logger.Info("volume transfer completed",
		zap.String("volume_id", volumeID),
		zap.String("transfer_id", transfer.ID),
	)

	return nil
}

// Ping pings the peer and measures latency
func (gc *GRPCClient) Ping(ctx context.Context) (*pb.Pong, time.Duration, error) {
	start := time.Now()

	pong, err := gc.client.Ping(ctx, &pb.Empty{})
	if err != nil {
		return nil, 0, fmt.Errorf("ping failed: %w", err)
	}

	latency := time.Since(start)

	return pong, latency, nil
}

// Close closes the gRPC connection
func (gc *GRPCClient) Close() error {
	if gc.conn != nil {
		gc.logger.Info("closing gRPC client")
		return gc.conn.Close()
	}
	return nil
}
