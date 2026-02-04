package worker

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/artemis/docker-migrate/internal/peer"
	pb "github.com/artemis/docker-migrate/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Connector manages the connection to the master
type Connector struct {
	worker        *Worker
	cryptoManager *peer.CryptoManager
	logger        *observability.Logger

	conn   *grpc.ClientConn
	client pb.MasterServiceClient
	stream pb.MasterService_WorkerStreamClient

	heartbeatInterval time.Duration
	inventoryInterval time.Duration

	mu        sync.RWMutex
	connected bool
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewConnector creates a new connector
func NewConnector(worker *Worker, cryptoManager *peer.CryptoManager, logger *observability.Logger) *Connector {
	return &Connector{
		worker:            worker,
		cryptoManager:     cryptoManager,
		logger:            logger,
		heartbeatInterval: 10 * time.Second,
		inventoryInterval: 60 * time.Second,
	}
}

// Connect connects to the master and registers
func (c *Connector) Connect(ctx context.Context, enrollmentToken string) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Get TLS config
	tlsConfig, err := c.cryptoManager.GetClientTLSConfig()
	if err != nil {
		return fmt.Errorf("failed to get TLS config: %w", err)
	}
	tlsConfig.InsecureSkipVerify = true // Master cert may not be in trust store yet

	// Connect with retry loop
	return c.connectWithRetry(enrollmentToken, tlsConfig)
}

func (c *Connector) connectWithRetry(enrollmentToken string, tlsConfig *tls.Config) error {
	cfg := c.worker.GetConfig().Worker
	backoff := cfg.ReconnectInterval
	maxBackoff := cfg.MaxReconnectInterval

	for {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		default:
		}

		err := c.doConnect(enrollmentToken, tlsConfig)
		if err == nil {
			// Connected successfully, start maintenance loops
			go c.heartbeatLoop()
			go c.inventoryLoop()
			go c.receiveLoop()
			return nil
		}

		c.logger.Warn("connection to master failed, retrying",
			zap.Error(err),
			zap.Duration("backoff", backoff),
		)

		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case <-time.After(backoff):
		}

		// Exponential backoff
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (c *Connector) doConnect(enrollmentToken string, tlsConfig *tls.Config) error {
	cfg := c.worker.GetConfig()
	masterURL := cfg.Worker.MasterURL

	c.logger.Info("connecting to master", zap.String("url", masterURL))

	// Create gRPC connection
	conn, err := grpc.Dial(
		masterURL,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	)
	if err != nil {
		return fmt.Errorf("failed to dial master: %w", err)
	}

	c.conn = conn
	c.client = pb.NewMasterServiceClient(conn)

	// Get hostname
	hostname, _ := os.Hostname()

	// Get TLS fingerprint
	fingerprint := c.cryptoManager.GetFingerprint()
	if fingerprint == "" {
		conn.Close()
		return fmt.Errorf("failed to get fingerprint: certificate not initialized")
	}

	// Register with master
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	resp, err := c.client.RegisterWorker(ctx, &pb.WorkerRegistration{
		EnrollmentToken: enrollmentToken,
		WorkerName:      cfg.Worker.Name,
		Hostname:        hostname,
		GrpcAddress:     cfg.GRPCAddr,
		TlsFingerprint:  fingerprint,
		Labels:          cfg.Worker.Labels,
		Version:         "1.0.0", // TODO: get from build
	})
	if err != nil {
		conn.Close()
		return fmt.Errorf("registration failed: %w", err)
	}

	if !resp.Success {
		conn.Close()
		return fmt.Errorf("registration rejected: %s", resp.Error)
	}

	// Store credentials
	c.worker.SetCredentials(resp.WorkerId, resp.AuthToken)

	// Update intervals from master
	if resp.HeartbeatIntervalMs > 0 {
		c.heartbeatInterval = time.Duration(resp.HeartbeatIntervalMs) * time.Millisecond
	}
	if resp.InventoryIntervalMs > 0 {
		c.inventoryInterval = time.Duration(resp.InventoryIntervalMs) * time.Millisecond
	}

	// Open bidirectional stream
	stream, err := c.client.WorkerStream(c.ctx)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open stream: %w", err)
	}

	c.stream = stream
	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()

	c.logger.Info("connected to master",
		zap.String("worker_id", resp.WorkerId),
		zap.Duration("heartbeat_interval", c.heartbeatInterval),
	)

	return nil
}

// Disconnect disconnects from the master
func (c *Connector) Disconnect() {
	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}
	if c.stream != nil {
		c.stream.CloseSend()
	}
	if c.conn != nil {
		c.conn.Close()
	}
}

// IsConnected returns whether connected to master
func (c *Connector) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *Connector) heartbeatLoop() {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.sendHeartbeat()
		}
	}
}

func (c *Connector) sendHeartbeat() {
	workerID, authToken := c.worker.GetCredentials()

	msg := &pb.WorkerMessage{
		WorkerId:  workerID,
		AuthToken: authToken,
		Payload: &pb.WorkerMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				Timestamp:        time.Now().UnixMilli(),
				Status:           pb.WorkerStatus_WORKER_STATUS_IDLE,
				ActiveMigrations: 0,
				SystemResources:  c.getSystemResources(),
			},
		},
	}

	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	if stream != nil {
		if err := stream.Send(msg); err != nil {
			c.logger.Error("failed to send heartbeat", zap.Error(err))
			c.handleDisconnect()
		}
	}
}

func (c *Connector) getSystemResources() *pb.SystemResources {
	// TODO: implement actual system resource collection
	return &pb.SystemResources{
		CpuPercent:      0,
		MemoryTotal:     0,
		MemoryAvailable: 0,
		DiskTotal:       0,
		DiskAvailable:   0,
	}
}

func (c *Connector) inventoryLoop() {
	// Send initial inventory immediately
	c.sendInventory()

	ticker := time.NewTicker(c.inventoryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.sendInventory()
		}
	}
}

func (c *Connector) sendInventory() {
	workerID, authToken := c.worker.GetCredentials()

	inv, err := c.worker.inventory.Scan(c.ctx)
	if err != nil {
		c.logger.Error("failed to scan inventory", zap.Error(err))
		return
	}

	inv.WorkerId = workerID
	inv.AuthToken = authToken
	inv.Timestamp = time.Now().UnixMilli()

	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	_, err = c.client.ReportResources(ctx, inv)
	if err != nil {
		c.logger.Error("failed to report inventory", zap.Error(err))
	}
}

func (c *Connector) receiveLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		c.mu.RLock()
		stream := c.stream
		c.mu.RUnlock()

		if stream == nil {
			return
		}

		cmd, err := stream.Recv()
		if err == io.EOF {
			c.logger.Info("stream closed by master")
			c.handleDisconnect()
			return
		}
		if err != nil {
			c.logger.Error("stream receive error", zap.Error(err))
			c.handleDisconnect()
			return
		}

		c.handleCommand(cmd)
	}
}

func (c *Connector) handleCommand(cmd *pb.MasterCommand) {
	switch payload := cmd.Payload.(type) {
	case *pb.MasterCommand_HeartbeatAck:
		// Heartbeat acknowledged, nothing to do

	case *pb.MasterCommand_StartMigration:
		c.handleStartMigration(payload.StartMigration)

	case *pb.MasterCommand_CancelMigration:
		c.handleCancelMigration(payload.CancelMigration)

	case *pb.MasterCommand_UpdateConfig:
		c.handleUpdateConfig(payload.UpdateConfig)

	case *pb.MasterCommand_Shutdown:
		c.logger.Info("shutdown command received", zap.String("reason", payload.Shutdown.Reason))
		c.worker.Stop()
	}
}

func (c *Connector) handleStartMigration(cmd *pb.StartMigrationCommand) {
	c.logger.Info("migration command received",
		zap.String("role", cmd.Role.String()),
	)

	switch cmd.Role {
	case pb.MigrationRole_MIGRATION_ROLE_SOURCE:
		go c.worker.executor.ExecuteAsSource(c.ctx, cmd.Request, c.stream)

	case pb.MigrationRole_MIGRATION_ROLE_TARGET:
		go c.worker.executor.ExecuteAsTarget(c.ctx, cmd.AcceptRequest, c.stream)
	}
}

func (c *Connector) handleCancelMigration(cmd *pb.CancelMigrationCommand) {
	c.logger.Info("cancel migration command received",
		zap.String("migration_id", cmd.MigrationId),
	)
	c.worker.executor.Cancel(cmd.MigrationId)
}

func (c *Connector) handleUpdateConfig(cmd *pb.UpdateConfigCommand) {
	if cmd.HeartbeatIntervalMs > 0 {
		c.heartbeatInterval = time.Duration(cmd.HeartbeatIntervalMs) * time.Millisecond
	}
	if cmd.InventoryIntervalMs > 0 {
		c.inventoryInterval = time.Duration(cmd.InventoryIntervalMs) * time.Millisecond
	}
}

func (c *Connector) handleDisconnect() {
	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()

	// Trigger reconnection
	go c.reconnect()
}

func (c *Connector) reconnect() {
	c.logger.Info("attempting to reconnect to master")

	// Get stored auth token for reconnection
	_, authToken := c.worker.GetCredentials()

	tlsConfig, err := c.cryptoManager.GetClientTLSConfig()
	if err != nil {
		c.logger.Error("failed to get TLS config for reconnect", zap.Error(err))
		return
	}
	tlsConfig.InsecureSkipVerify = true

	// Use auth token as enrollment token for re-registration
	c.connectWithRetry(authToken, tlsConfig)
}

// SendProgress sends migration progress to master
func (c *Connector) SendProgress(progress *pb.MigrationProgress) error {
	workerID, authToken := c.worker.GetCredentials()

	msg := &pb.WorkerMessage{
		WorkerId:  workerID,
		AuthToken: authToken,
		Payload: &pb.WorkerMessage_MigrationProgress{
			MigrationProgress: progress,
		},
	}

	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	if stream == nil {
		return fmt.Errorf("not connected")
	}

	return stream.Send(msg)
}

// SendComplete sends migration completion to master
func (c *Connector) SendComplete(complete *pb.MigrationComplete) error {
	workerID, authToken := c.worker.GetCredentials()

	msg := &pb.WorkerMessage{
		WorkerId:  workerID,
		AuthToken: authToken,
		Payload: &pb.WorkerMessage_MigrationComplete{
			MigrationComplete: complete,
		},
	}

	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	if stream == nil {
		return fmt.Errorf("not connected")
	}

	return stream.Send(msg)
}
