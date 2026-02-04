package master

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/artemis/docker-migrate/internal/observability"
	pb "github.com/artemis/docker-migrate/proto"
	"go.uber.org/zap"
)

// ProxyManager handles relay of migration data through the master
type ProxyManager struct {
	pb.UnimplementedProxyServiceServer

	registry *Registry
	logger   *observability.Logger
	channels map[string]*ProxyChannel // migration_id -> channel
	mu       sync.RWMutex
}

// ProxyChannel represents an active proxy session for a migration
type ProxyChannel struct {
	MigrationID  string
	SourceStream pb.ProxyService_OpenProxyChannelServer
	TargetStream pb.ProxyService_OpenProxyChannelServer
	SourceReady  chan struct{}
	TargetReady  chan struct{}
	BytesRelayed int64
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
}

// NewProxyManager creates a new ProxyManager
func NewProxyManager(registry *Registry, logger *observability.Logger) *ProxyManager {
	return &ProxyManager{
		registry: registry,
		logger:   logger,
		channels: make(map[string]*ProxyChannel),
	}
}

// OpenProxyChannel implements ProxyServiceServer - workers connect here to relay data
func (pm *ProxyManager) OpenProxyChannel(stream pb.ProxyService_OpenProxyChannelServer) error {
	// 1. Receive handshake to get migration_id and role
	msg, err := stream.Recv()
	if err != nil {
		pm.logger.Error("failed to receive handshake", zap.Error(err))
		return fmt.Errorf("failed to receive handshake: %w", err)
	}

	if msg.Type != pb.ProxyDataType_PROXY_DATA_HANDSHAKE {
		pm.logger.Error("expected handshake message", zap.String("type", msg.Type.String()))
		return fmt.Errorf("expected handshake message, got %s", msg.Type.String())
	}

	handshake := msg.GetHandshake()
	if handshake == nil {
		pm.logger.Error("handshake payload is nil")
		return fmt.Errorf("handshake payload is nil")
	}

	migrationID := msg.MigrationId
	workerID := msg.WorkerId
	role := handshake.Role

	pm.logger.Info("proxy channel handshake received",
		zap.String("migration_id", migrationID),
		zap.String("worker_id", workerID),
		zap.String("role", role.String()),
	)

	// Validate worker auth token
	worker, ok := pm.registry.GetByAuthToken(handshake.AuthToken)
	if !ok {
		pm.logger.Warn("invalid auth token in proxy handshake",
			zap.String("worker_id", workerID),
		)
		return fmt.Errorf("invalid auth token")
	}

	if worker.ID != workerID {
		pm.logger.Warn("worker ID mismatch in proxy handshake",
			zap.String("claimed", workerID),
			zap.String("actual", worker.ID),
		)
		return fmt.Errorf("worker ID mismatch")
	}

	// 2. Get or create ProxyChannel for migration_id
	channel := pm.getOrCreateChannel(migrationID)

	// 3. Register stream as source or target based on role
	channel.mu.Lock()
	switch role {
	case pb.ProxyRole_PROXY_ROLE_SOURCE:
		if channel.SourceStream != nil {
			channel.mu.Unlock()
			pm.logger.Warn("source stream already registered",
				zap.String("migration_id", migrationID),
			)
			return fmt.Errorf("source stream already registered for migration %s", migrationID)
		}
		channel.SourceStream = stream
		close(channel.SourceReady)
		pm.logger.Info("source stream registered",
			zap.String("migration_id", migrationID),
			zap.String("worker_id", workerID),
		)

	case pb.ProxyRole_PROXY_ROLE_TARGET:
		if channel.TargetStream != nil {
			channel.mu.Unlock()
			pm.logger.Warn("target stream already registered",
				zap.String("migration_id", migrationID),
			)
			return fmt.Errorf("target stream already registered for migration %s", migrationID)
		}
		channel.TargetStream = stream
		close(channel.TargetReady)
		pm.logger.Info("target stream registered",
			zap.String("migration_id", migrationID),
			zap.String("worker_id", workerID),
		)

	default:
		channel.mu.Unlock()
		return fmt.Errorf("unknown proxy role: %s", role.String())
	}
	channel.mu.Unlock()

	// 4. Wait for both streams to be ready
	select {
	case <-channel.SourceReady:
	case <-channel.ctx.Done():
		return channel.ctx.Err()
	}

	select {
	case <-channel.TargetReady:
	case <-channel.ctx.Done():
		return channel.ctx.Err()
	}

	pm.logger.Info("both streams ready, starting relay",
		zap.String("migration_id", migrationID),
	)

	// 5. Start relay loop
	// The relay loop runs in separate goroutines
	// This goroutine will be one of the relay directions
	if role == pb.ProxyRole_PROXY_ROLE_SOURCE {
		// Source worker: relay from source -> target
		pm.relayLoop(channel)
	} else {
		// Target worker: wait for context to be done
		// The relay loop is handled by the source goroutine
		<-channel.ctx.Done()
	}

	// 6. Block until completion or error
	pm.logger.Info("proxy channel completed",
		zap.String("migration_id", migrationID),
		zap.String("role", role.String()),
		zap.Int64("bytes_relayed", atomic.LoadInt64(&channel.BytesRelayed)),
	)

	return nil
}

// getOrCreateChannel gets or creates a ProxyChannel for the given migration ID
func (pm *ProxyManager) getOrCreateChannel(migrationID string) *ProxyChannel {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if channel, ok := pm.channels[migrationID]; ok {
		return channel
	}

	ctx, cancel := context.WithCancel(context.Background())
	channel := &ProxyChannel{
		MigrationID: migrationID,
		SourceReady: make(chan struct{}),
		TargetReady: make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}
	pm.channels[migrationID] = channel

	pm.logger.Debug("created proxy channel",
		zap.String("migration_id", migrationID),
	)

	return channel
}

// relayLoop relays data bidirectionally between source and target streams
func (pm *ProxyManager) relayLoop(channel *ProxyChannel) {
	var wg sync.WaitGroup
	wg.Add(2)

	errChan := make(chan error, 2)

	// Goroutine 1: source -> target (for data)
	go func() {
		defer wg.Done()
		err := pm.relaySourceToTarget(channel)
		if err != nil && err != io.EOF {
			errChan <- fmt.Errorf("source->target relay error: %w", err)
		}
	}()

	// Goroutine 2: target -> source (for acks)
	go func() {
		defer wg.Done()
		err := pm.relayTargetToSource(channel)
		if err != nil && err != io.EOF {
			errChan <- fmt.Errorf("target->source relay error: %w", err)
		}
	}()

	// Wait for both goroutines to complete or handle first error
	go func() {
		wg.Wait()
		close(errChan)
		pm.cleanupChannel(channel.MigrationID)
	}()

	// Wait for completion or error
	select {
	case err := <-errChan:
		if err != nil {
			pm.logger.Error("relay loop error",
				zap.String("migration_id", channel.MigrationID),
				zap.Error(err),
			)
			channel.cancel()
		}
	case <-channel.ctx.Done():
		pm.logger.Debug("relay loop context done",
			zap.String("migration_id", channel.MigrationID),
		)
	}
}

// relaySourceToTarget relays data from source stream to target stream
func (pm *ProxyManager) relaySourceToTarget(channel *ProxyChannel) error {
	for {
		select {
		case <-channel.ctx.Done():
			return channel.ctx.Err()
		default:
		}

		msg, err := channel.SourceStream.Recv()
		if err == io.EOF {
			pm.logger.Debug("source stream EOF",
				zap.String("migration_id", channel.MigrationID),
			)
			return io.EOF
		}
		if err != nil {
			pm.logger.Error("error receiving from source",
				zap.String("migration_id", channel.MigrationID),
				zap.Error(err),
			)
			return err
		}

		// Check for close message
		if msg.Type == pb.ProxyDataType_PROXY_DATA_CLOSE {
			pm.logger.Info("received close from source",
				zap.String("migration_id", channel.MigrationID),
			)
			// Forward close to target
			if err := channel.TargetStream.Send(msg); err != nil {
				pm.logger.Warn("failed to forward close to target",
					zap.String("migration_id", channel.MigrationID),
					zap.Error(err),
				)
			}
			channel.cancel()
			return nil
		}

		// Track bytes for data messages
		var dataSize int
		switch msg.Type {
		case pb.ProxyDataType_PROXY_DATA_VOLUME:
			if chunk := msg.GetVolumeChunk(); chunk != nil {
				dataSize = len(chunk.Data)
			}
		case pb.ProxyDataType_PROXY_DATA_IMAGE:
			if blob := msg.GetLayerBlob(); blob != nil {
				dataSize = len(blob.Data)
			}
		case pb.ProxyDataType_PROXY_DATA_CONTAINER:
			if chunk := msg.GetContainerChunk(); chunk != nil {
				dataSize = len(chunk.StateData)
			}
		}

		// Forward to target
		if err := channel.TargetStream.Send(msg); err != nil {
			pm.logger.Error("error sending to target",
				zap.String("migration_id", channel.MigrationID),
				zap.Error(err),
			)
			return err
		}

		if dataSize > 0 {
			atomic.AddInt64(&channel.BytesRelayed, int64(dataSize))
			pm.logger.Debug("relayed data source->target",
				zap.String("migration_id", channel.MigrationID),
				zap.Int("bytes", dataSize),
				zap.String("type", msg.Type.String()),
			)
		}
	}
}

// relayTargetToSource relays acks from target stream to source stream
func (pm *ProxyManager) relayTargetToSource(channel *ProxyChannel) error {
	for {
		select {
		case <-channel.ctx.Done():
			return channel.ctx.Err()
		default:
		}

		msg, err := channel.TargetStream.Recv()
		if err == io.EOF {
			pm.logger.Debug("target stream EOF",
				zap.String("migration_id", channel.MigrationID),
			)
			return io.EOF
		}
		if err != nil {
			pm.logger.Error("error receiving from target",
				zap.String("migration_id", channel.MigrationID),
				zap.Error(err),
			)
			return err
		}

		// Check for close message
		if msg.Type == pb.ProxyDataType_PROXY_DATA_CLOSE {
			pm.logger.Info("received close from target",
				zap.String("migration_id", channel.MigrationID),
			)
			// Forward close to source
			if err := channel.SourceStream.Send(msg); err != nil {
				pm.logger.Warn("failed to forward close to source",
					zap.String("migration_id", channel.MigrationID),
					zap.Error(err),
				)
			}
			channel.cancel()
			return nil
		}

		// Forward ack to source
		if err := channel.SourceStream.Send(msg); err != nil {
			pm.logger.Error("error sending to source",
				zap.String("migration_id", channel.MigrationID),
				zap.Error(err),
			)
			return err
		}

		pm.logger.Debug("relayed ack target->source",
			zap.String("migration_id", channel.MigrationID),
			zap.String("type", msg.Type.String()),
		)
	}
}

// cleanupChannel removes a channel from the map and cleans up resources
func (pm *ProxyManager) cleanupChannel(migrationID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if channel, ok := pm.channels[migrationID]; ok {
		// Cancel context if not already done
		channel.cancel()

		pm.logger.Info("cleaning up proxy channel",
			zap.String("migration_id", migrationID),
			zap.Int64("total_bytes_relayed", atomic.LoadInt64(&channel.BytesRelayed)),
		)

		delete(pm.channels, migrationID)
	}
}

// GetChannel returns a proxy channel by migration ID (for testing/monitoring)
func (pm *ProxyManager) GetChannel(migrationID string) (*ProxyChannel, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	channel, ok := pm.channels[migrationID]
	return channel, ok
}

// ActiveChannels returns the number of active proxy channels
func (pm *ProxyManager) ActiveChannels() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.channels)
}

// CancelChannel cancels a proxy channel by migration ID
func (pm *ProxyManager) CancelChannel(migrationID string, reason string) error {
	pm.mu.RLock()
	channel, ok := pm.channels[migrationID]
	pm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("proxy channel not found for migration %s", migrationID)
	}

	pm.logger.Info("cancelling proxy channel",
		zap.String("migration_id", migrationID),
		zap.String("reason", reason),
	)

	// Send close message to both streams
	closeMsg := &pb.ProxyData{
		MigrationId: migrationID,
		Type:        pb.ProxyDataType_PROXY_DATA_CLOSE,
		Payload: &pb.ProxyData_Close{
			Close: &pb.ProxyClose{
				Success: false,
				Error:   reason,
			},
		},
	}

	channel.mu.Lock()
	if channel.SourceStream != nil {
		_ = channel.SourceStream.Send(closeMsg)
	}
	if channel.TargetStream != nil {
		_ = channel.TargetStream.Send(closeMsg)
	}
	channel.mu.Unlock()

	channel.cancel()
	return nil
}
