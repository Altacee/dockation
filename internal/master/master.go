package master

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/config"
	"github.com/artemis/docker-migrate/internal/docker"
	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/artemis/docker-migrate/internal/peer"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// Master represents the master node
type Master struct {
	config          *config.Config
	docker          *docker.Client
	cryptoManager   *peer.CryptoManager
	transferManager *peer.TransferManager
	logger          *observability.Logger

	registry     *Registry
	orchestrator *Orchestrator
	grpcServer   *GRPCServer

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a new master node
func New(
	cfg *config.Config,
	dockerClient *docker.Client,
	cryptoManager *peer.CryptoManager,
	transferManager *peer.TransferManager,
	logger *observability.Logger,
) (*Master, error) {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Master{
		config:          cfg,
		docker:          dockerClient,
		cryptoManager:   cryptoManager,
		transferManager: transferManager,
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
	}

	// Initialize registry
	m.registry = NewRegistry(logger, cfg.Master.WorkerTimeout)

	// Initialize orchestrator
	m.orchestrator = NewOrchestrator(m.registry, logger)

	// Initialize gRPC server
	var err error
	m.grpcServer, err = NewGRPCServer(m, cryptoManager, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create gRPC server: %w", err)
	}

	return m, nil
}

// RegisterGRPCService registers the MasterService on an existing gRPC server
func (m *Master) RegisterGRPCService(server *grpc.Server) {
	m.grpcServer.RegisterOn(server)
	m.logger.Info("master gRPC service registered")
}

// StartBackgroundTasks starts background tasks like registry cleanup
func (m *Master) StartBackgroundTasks(ctx context.Context) {
	m.registry.StartCleanup(ctx, m.config.Master.WorkerTimeout/2)
}

// Start starts the master node with its own gRPC server (standalone mode)
func (m *Master) Start(ctx context.Context) error {
	m.logger.Info("starting master node",
		zap.String("grpc_addr", m.config.GRPCAddr),
	)

	// Start registry cleanup goroutine
	go m.registry.StartCleanup(ctx, m.config.Master.WorkerTimeout/2)

	// Start gRPC server
	if err := m.grpcServer.Start(m.config.GRPCAddr); err != nil {
		return fmt.Errorf("gRPC server failed: %w", err)
	}

	return nil
}

// Stop stops the master node
func (m *Master) Stop() {
	m.logger.Info("stopping master node")
	m.cancel()
	m.grpcServer.Stop()
}

// GetRegistry returns the worker registry
func (m *Master) GetRegistry() *Registry {
	return m.registry
}

// GetOrchestrator returns the migration orchestrator
func (m *Master) GetOrchestrator() *Orchestrator {
	return m.orchestrator
}

// GetConfig returns the config
func (m *Master) GetConfig() *config.Config {
	return m.config
}

// ValidateEnrollmentToken checks if the token is valid
func (m *Master) ValidateEnrollmentToken(token string) bool {
	return m.config.Master != nil && m.config.Master.EnrollmentToken == token
}

// GenerateWorkerAuthToken generates a new auth token for a worker
func (m *Master) GenerateWorkerAuthToken() string {
	return generateToken(32)
}

func generateToken(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to time-based generation if crypto/rand fails
		for i := range bytes {
			bytes[i] = byte(time.Now().UnixNano() % 256)
			time.Sleep(time.Nanosecond)
		}
	}
	return hex.EncodeToString(bytes)
}
