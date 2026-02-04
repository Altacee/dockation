package worker

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/docker"
	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/artemis/docker-migrate/internal/peer"
	pb "github.com/artemis/docker-migrate/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// CredentialsProvider provides worker credentials for authentication
type CredentialsProvider interface {
	GetCredentials() (workerID, authToken string)
}

// Executor handles migration execution
type Executor struct {
	docker          *docker.Client
	transferManager *peer.TransferManager
	cryptoManager   *peer.CryptoManager
	logger          *observability.Logger
	credentials     CredentialsProvider

	activeMigrations map[string]context.CancelFunc
	mu               sync.RWMutex
}

// NewExecutor creates a new migration executor
func NewExecutor(
	dockerClient *docker.Client,
	transferManager *peer.TransferManager,
	cryptoManager *peer.CryptoManager,
	logger *observability.Logger,
) *Executor {
	return &Executor{
		docker:           dockerClient,
		transferManager:  transferManager,
		cryptoManager:    cryptoManager,
		logger:           logger,
		activeMigrations: make(map[string]context.CancelFunc),
	}
}

// SetCredentialsProvider sets the credentials provider for authentication
func (e *Executor) SetCredentialsProvider(provider CredentialsProvider) {
	e.credentials = provider
}

// ExecuteAsSource executes migration as the source (sender)
func (e *Executor) ExecuteAsSource(ctx context.Context, req *pb.MigrationRequest, stream pb.MasterService_WorkerStreamClient) {
	migrationID := req.MigrationId

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.activeMigrations[migrationID] = cancel
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		delete(e.activeMigrations, migrationID)
		e.mu.Unlock()
	}()

	e.logger.Info("executing migration as source",
		zap.String("migration_id", migrationID),
		zap.String("target", req.TargetAddress),
	)

	startTime := time.Now()
	var totalBytes int64

	// Create transfer client based on mode
	var client TransferClient
	var err error

	switch req.TransferMode {
	case pb.TransferMode_TRANSFER_MODE_PROXY:
		client, err = e.createProxyClient(ctx, req)
	default:
		client, err = e.createDirectClient(ctx, req)
	}
	if err != nil {
		e.sendComplete(stream, migrationID, false, err.Error(), 0)
		return
	}
	defer client.Close()

	// Transfer volumes
	e.sendProgress(stream, migrationID, pb.MigrationPhase_MIGRATION_PHASE_TRANSFERRING_VOLUMES, 0, 0, 0)
	for i, volName := range req.VolumeNames {
		select {
		case <-ctx.Done():
			e.sendComplete(stream, migrationID, false, "cancelled", totalBytes)
			return
		default:
		}

		bytes, err := e.transferVolume(ctx, client, volName)
		if err != nil {
			e.sendComplete(stream, migrationID, false, fmt.Sprintf("volume transfer failed: %v", err), totalBytes)
			return
		}
		totalBytes += bytes

		progress := float32(i+1) / float32(len(req.VolumeNames))
		e.sendProgress(stream, migrationID, pb.MigrationPhase_MIGRATION_PHASE_TRANSFERRING_VOLUMES, progress, totalBytes, 0)
	}

	// Transfer images
	e.sendProgress(stream, migrationID, pb.MigrationPhase_MIGRATION_PHASE_TRANSFERRING_IMAGES, 0, totalBytes, 0)
	for i, imageID := range req.ImageIds {
		select {
		case <-ctx.Done():
			e.sendComplete(stream, migrationID, false, "cancelled", totalBytes)
			return
		default:
		}

		bytes, err := e.transferImage(ctx, client, imageID)
		if err != nil {
			e.sendComplete(stream, migrationID, false, fmt.Sprintf("image transfer failed: %v", err), totalBytes)
			return
		}
		totalBytes += bytes

		progress := float32(i+1) / float32(len(req.ImageIds))
		e.sendProgress(stream, migrationID, pb.MigrationPhase_MIGRATION_PHASE_TRANSFERRING_IMAGES, progress, totalBytes, 0)
	}

	// Mark complete
	e.sendProgress(stream, migrationID, pb.MigrationPhase_MIGRATION_PHASE_FINALIZING, 1.0, totalBytes, 0)

	duration := time.Since(startTime).Milliseconds()
	e.logger.Info("migration complete as source",
		zap.String("migration_id", migrationID),
		zap.Int64("bytes", totalBytes),
		zap.Int64("duration_ms", duration),
	)

	e.sendComplete(stream, migrationID, true, "", totalBytes)
}

// ExecuteAsTarget executes migration as the target (receiver)
func (e *Executor) ExecuteAsTarget(ctx context.Context, req *pb.AcceptMigrationRequest, stream pb.MasterService_WorkerStreamClient) {
	if req.TransferMode == pb.TransferMode_TRANSFER_MODE_PROXY {
		e.executeTargetViaProxy(ctx, req, stream)
		return
	}

	// Direct mode: target is passive - receives data via MigrationService gRPC
	migrationID := req.MigrationId

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.activeMigrations[migrationID] = cancel
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		delete(e.activeMigrations, migrationID)
		e.mu.Unlock()
	}()

	e.logger.Info("ready to accept migration as target",
		zap.String("migration_id", migrationID),
		zap.String("source", req.SourceAddress),
	)

	// Target is mostly passive - it receives data via the MigrationService gRPC
	// The source will connect and stream data to us
	// We just wait for completion or cancellation

	<-ctx.Done()
}

func (e *Executor) executeTargetViaProxy(ctx context.Context, req *pb.AcceptMigrationRequest, masterStream pb.MasterService_WorkerStreamClient) {
	migrationID := req.MigrationId

	// Setup cancellation
	ctx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.activeMigrations[migrationID] = cancel
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		delete(e.activeMigrations, migrationID)
		e.mu.Unlock()
	}()

	e.logger.Info("connecting to proxy as target",
		zap.String("migration_id", migrationID),
		zap.String("proxy_address", req.ProxyAddress),
	)

	// Connect to master's proxy
	tlsConfig, err := e.cryptoManager.GetClientTLSConfig()
	if err != nil {
		e.logger.Error("failed to get TLS config", zap.Error(err))
		return
	}
	tlsConfig.InsecureSkipVerify = true

	conn, err := grpc.Dial(req.ProxyAddress, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		e.logger.Error("failed to connect to proxy", zap.Error(err))
		return
	}
	defer conn.Close()

	proxyClient := pb.NewProxyServiceClient(conn)
	stream, err := proxyClient.OpenProxyChannel(ctx)
	if err != nil {
		e.logger.Error("failed to open proxy channel", zap.Error(err))
		return
	}

	// Send handshake as TARGET
	if err := stream.Send(&pb.ProxyData{
		MigrationId: migrationID,
		Type:        pb.ProxyDataType_PROXY_DATA_HANDSHAKE,
		Payload: &pb.ProxyData_Handshake{
			Handshake: &pb.ProxyHandshake{
				Role: pb.ProxyRole_PROXY_ROLE_TARGET,
			},
		},
	}); err != nil {
		e.logger.Error("failed to send proxy handshake", zap.Error(err))
		return
	}

	// Receive and process data from proxy
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, err := stream.Recv()
		if err == io.EOF {
			return
		}
		if err != nil {
			e.logger.Error("proxy receive error", zap.Error(err))
			return
		}

		// Handle based on data type
		switch data.Type {
		case pb.ProxyDataType_PROXY_DATA_VOLUME:
			chunk := data.GetVolumeChunk()
			if chunk != nil {
				// Process volume chunk (similar to TransferVolume receiver)
				// Send ack back
				stream.Send(&pb.ProxyData{
					MigrationId: migrationID,
					Type:        pb.ProxyDataType_PROXY_DATA_ACK,
					Payload: &pb.ProxyData_Ack{
						Ack: &pb.TransferAck{
							Offset:  chunk.Offset,
							Success: true,
						},
					},
				})
			}
		case pb.ProxyDataType_PROXY_DATA_IMAGE:
			blob := data.GetLayerBlob()
			if blob != nil {
				// Process image layer
				stream.Send(&pb.ProxyData{
					MigrationId: migrationID,
					Type:        pb.ProxyDataType_PROXY_DATA_ACK,
					Payload: &pb.ProxyData_Ack{
						Ack: &pb.TransferAck{
							Offset:  blob.Offset,
							Success: true,
						},
					},
				})
			}
		case pb.ProxyDataType_PROXY_DATA_CLOSE:
			closeMsg := data.GetClose()
			if closeMsg != nil {
				e.logger.Info("proxy transfer complete",
					zap.String("migration_id", migrationID),
					zap.Bool("success", closeMsg.Success),
				)
				return
			}
		}
	}
}

// Cancel cancels an active migration
func (e *Executor) Cancel(migrationID string) {
	e.mu.RLock()
	cancel, ok := e.activeMigrations[migrationID]
	e.mu.RUnlock()

	if ok {
		e.logger.Info("cancelling migration", zap.String("migration_id", migrationID))
		cancel()
	}
}

func (e *Executor) transferVolume(ctx context.Context, client TransferClient, volumeName string) (int64, error) {
	e.logger.Debug("transferring volume", zap.String("volume", volumeName))

	// Use the existing transfer manager for actual data transfer
	// This is a simplified version - full implementation would use streaming

	stream, err := client.TransferVolume(ctx)
	if err != nil {
		return 0, err
	}

	// Get volume data from Docker and stream it
	// For now, send a simple acknowledgment
	chunk := &pb.VolumeChunk{
		VolumeId:  volumeName,
		Offset:    0,
		Data:      []byte{}, // Would contain actual data
		Checksum:  "",
		TotalSize: 0,
		IsFinal:   true,
	}

	if err := stream.Send(chunk); err != nil {
		return 0, err
	}

	// Close send side and wait for final acknowledgment
	if err := stream.CloseSend(); err != nil {
		return 0, err
	}

	// Receive final ack
	_, err = stream.Recv()
	return 0, err
}

func (e *Executor) transferImage(ctx context.Context, client TransferClient, imageID string) (int64, error) {
	e.logger.Debug("transferring image", zap.String("image", imageID))

	stream, err := client.TransferImageLayers(ctx)
	if err != nil {
		return 0, err
	}

	// Send image layers
	blob := &pb.LayerBlob{
		ImageId:     imageID,
		LayerDigest: "",
		Offset:      0,
		Data:        []byte{},
		Checksum:    "",
		LayerSize:   0,
		IsFinal:     true,
	}

	if err := stream.Send(blob); err != nil {
		return 0, err
	}

	// Close send side and wait for final acknowledgment
	if err := stream.CloseSend(); err != nil {
		return 0, err
	}

	// Receive final ack
	_, err = stream.Recv()
	return 0, err
}

func (e *Executor) sendProgress(stream pb.MasterService_WorkerStreamClient, migrationID string, phase pb.MigrationPhase, progress float32, bytesTransferred, totalBytes int64) {
	var workerID, authToken string
	if e.credentials != nil {
		workerID, authToken = e.credentials.GetCredentials()
	}

	msg := &pb.WorkerMessage{
		WorkerId:  workerID,
		AuthToken: authToken,
		Payload: &pb.WorkerMessage_MigrationProgress{
			MigrationProgress: &pb.MigrationProgress{
				MigrationId:      migrationID,
				Phase:            phase,
				Progress:         progress,
				BytesTransferred: bytesTransferred,
				TotalBytes:       totalBytes,
			},
		},
	}
	stream.Send(msg)
}

func (e *Executor) sendComplete(stream pb.MasterService_WorkerStreamClient, migrationID string, success bool, errMsg string, bytesTransferred int64) {
	var workerID, authToken string
	if e.credentials != nil {
		workerID, authToken = e.credentials.GetCredentials()
	}

	msg := &pb.WorkerMessage{
		WorkerId:  workerID,
		AuthToken: authToken,
		Payload: &pb.WorkerMessage_MigrationComplete{
			MigrationComplete: &pb.MigrationComplete{
				MigrationId:      migrationID,
				Success:          success,
				Error:            errMsg,
				BytesTransferred: bytesTransferred,
			},
		},
	}
	stream.Send(msg)
}

// GetActiveMigrationCount returns the number of active migrations
func (e *Executor) GetActiveMigrationCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.activeMigrations)
}

func (e *Executor) createDirectClient(ctx context.Context, req *pb.MigrationRequest) (TransferClient, error) {
	tlsConfig, err := e.cryptoManager.GetClientTLSConfig()
	if err != nil {
		return nil, err
	}
	tlsConfig.InsecureSkipVerify = true

	conn, err := grpc.Dial(req.TargetAddress, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to target: %w", err)
	}

	return NewDirectTransferClient(pb.NewMigrationServiceClient(conn), conn), nil
}

func (e *Executor) createProxyClient(ctx context.Context, req *pb.MigrationRequest) (TransferClient, error) {
	tlsConfig, err := e.cryptoManager.GetClientTLSConfig()
	if err != nil {
		return nil, err
	}
	tlsConfig.InsecureSkipVerify = true

	conn, err := grpc.Dial(req.ProxyAddress, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to proxy: %w", err)
	}

	proxyClient := pb.NewProxyServiceClient(conn)
	stream, err := proxyClient.OpenProxyChannel(ctx)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open proxy channel: %w", err)
	}

	// Send handshake as SOURCE
	if err := stream.Send(&pb.ProxyData{
		MigrationId: req.MigrationId,
		Type:        pb.ProxyDataType_PROXY_DATA_HANDSHAKE,
		Payload: &pb.ProxyData_Handshake{
			Handshake: &pb.ProxyHandshake{
				Role:           pb.ProxyRole_PROXY_ROLE_SOURCE,
				TargetWorkerId: req.TargetWorkerId,
			},
		},
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send proxy handshake: %w", err)
	}

	return NewProxyTransferClient(stream, req.MigrationId, conn), nil
}
