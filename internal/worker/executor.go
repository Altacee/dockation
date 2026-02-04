package worker

import (
	"context"
	"fmt"
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

// Executor handles migration execution
type Executor struct {
	docker          *docker.Client
	transferManager *peer.TransferManager
	cryptoManager   *peer.CryptoManager
	logger          *observability.Logger

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

	// Connect to target worker
	tlsConfig, err := e.cryptoManager.GetClientTLSConfig()
	if err != nil {
		e.sendComplete(stream, migrationID, false, err.Error(), 0)
		return
	}
	tlsConfig.InsecureSkipVerify = true

	conn, err := grpc.Dial(
		req.TargetAddress,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	)
	if err != nil {
		e.sendComplete(stream, migrationID, false, fmt.Sprintf("failed to connect to target: %v", err), 0)
		return
	}
	defer conn.Close()

	targetClient := pb.NewMigrationServiceClient(conn)

	// Transfer volumes
	e.sendProgress(stream, migrationID, pb.MigrationPhase_MIGRATION_PHASE_TRANSFERRING_VOLUMES, 0, 0, 0)
	for i, volName := range req.VolumeNames {
		select {
		case <-ctx.Done():
			e.sendComplete(stream, migrationID, false, "cancelled", totalBytes)
			return
		default:
		}

		bytes, err := e.transferVolume(ctx, targetClient, volName)
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

		bytes, err := e.transferImage(ctx, targetClient, imageID)
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

func (e *Executor) transferVolume(ctx context.Context, client pb.MigrationServiceClient, volumeName string) (int64, error) {
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

func (e *Executor) transferImage(ctx context.Context, client pb.MigrationServiceClient, imageID string) (int64, error) {
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
	// Note: In a real implementation, we'd get workerID/authToken from the worker instance
	// For now this is a simplified version
	msg := &pb.WorkerMessage{
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
	msg := &pb.WorkerMessage{
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
