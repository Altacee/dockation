package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/config"
	"github.com/artemis/docker-migrate/internal/docker"
	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/artemis/docker-migrate/internal/peer"
	"go.uber.org/zap"
)

// Worker represents a worker node
type Worker struct {
	config          *config.Config
	docker          *docker.Client
	cryptoManager   *peer.CryptoManager
	transferManager *peer.TransferManager
	logger          *observability.Logger

	connector  *Connector
	inventory  *Inventory
	executor   *Executor
	grpcServer *GRPCServer

	workerID  string
	authToken string

	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	startTime time.Time
}

// New creates a new worker
func New(
	cfg *config.Config,
	dockerClient *docker.Client,
	cryptoManager *peer.CryptoManager,
	transferManager *peer.TransferManager,
	logger *observability.Logger,
) (*Worker, error) {
	ctx, cancel := context.WithCancel(context.Background())

	w := &Worker{
		config:          cfg,
		docker:          dockerClient,
		cryptoManager:   cryptoManager,
		transferManager: transferManager,
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
		startTime:       time.Now(),
	}

	// Initialize inventory scanner
	w.inventory = NewInventory(dockerClient, logger)

	// Initialize migration executor
	w.executor = NewExecutor(dockerClient, transferManager, cryptoManager, logger)
	w.executor.SetCredentialsProvider(w)

	// Initialize gRPC server for WorkerService
	var err error
	w.grpcServer, err = NewGRPCServer(w, cryptoManager, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create gRPC server: %w", err)
	}

	return w, nil
}

// Start starts the worker and connects to the master
func (w *Worker) Start(ctx context.Context, enrollmentToken string) error {
	w.logger.Info("starting worker",
		zap.String("name", w.config.Worker.Name),
		zap.String("master_url", w.config.Worker.MasterURL),
	)

	// Start local gRPC server for incoming migration connections
	go func() {
		if err := w.grpcServer.Start(w.config.GRPCAddr); err != nil {
			w.logger.Error("gRPC server error", zap.Error(err))
		}
	}()

	// Create connector and connect to master
	w.connector = NewConnector(w, w.cryptoManager, w.logger)

	// Connect and register with master
	if err := w.connector.Connect(ctx, enrollmentToken); err != nil {
		return fmt.Errorf("failed to connect to master: %w", err)
	}

	// Block until context is cancelled
	<-ctx.Done()

	return nil
}

// Stop stops the worker
func (w *Worker) Stop() {
	w.logger.Info("stopping worker")
	w.cancel()

	if w.connector != nil {
		w.connector.Disconnect()
	}
	if w.grpcServer != nil {
		w.grpcServer.Stop()
	}
}

// GetConfig returns the config
func (w *Worker) GetConfig() *config.Config {
	return w.config
}

// GetDocker returns the Docker client
func (w *Worker) GetDocker() *docker.Client {
	return w.docker
}

// GetInventory returns the inventory scanner
func (w *Worker) GetInventory() *Inventory {
	return w.inventory
}

// GetExecutor returns the migration executor
func (w *Worker) GetExecutor() *Executor {
	return w.executor
}

// GetTransferManager returns the transfer manager
func (w *Worker) GetTransferManager() *peer.TransferManager {
	return w.transferManager
}

// SetCredentials stores the worker ID and auth token from registration
func (w *Worker) SetCredentials(workerID, authToken string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.workerID = workerID
	w.authToken = authToken

	// Also store in config for persistence
	w.config.SetWorkerCredentials(workerID, authToken)
}

// GetCredentials returns the worker ID and auth token
func (w *Worker) GetCredentials() (string, string) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.workerID, w.authToken
}

// GetUptime returns how long the worker has been running
func (w *Worker) GetUptime() time.Duration {
	return time.Since(w.startTime)
}
