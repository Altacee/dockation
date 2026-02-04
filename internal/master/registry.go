package master

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/observability"
	pb "github.com/artemis/docker-migrate/proto"
	"go.uber.org/zap"
)

// WorkerInfo holds information about a connected worker
type WorkerInfo struct {
	ID             string
	Name           string
	Hostname       string
	GRPCAddress    string
	TLSFingerprint string
	Labels         map[string]string
	Version        string

	Status    pb.WorkerStatus
	AuthToken string

	RegisteredAt  time.Time
	LastHeartbeat time.Time
	LastInventory time.Time

	// Resource inventory
	Containers []*pb.ContainerResource
	Images     []*pb.ImageResource
	Volumes    []*pb.VolumeResource
	Networks   []*pb.NetworkResource

	// System resources
	SystemResources *pb.SystemResources

	// Stream for sending commands
	stream   pb.MasterService_WorkerStreamServer
	streamMu sync.Mutex
}

// Registry manages connected workers
type Registry struct {
	workers map[string]*WorkerInfo
	mu      sync.RWMutex
	logger  *observability.Logger
	timeout time.Duration
}

// NewRegistry creates a new worker registry
func NewRegistry(logger *observability.Logger, timeout time.Duration) *Registry {
	return &Registry{
		workers: make(map[string]*WorkerInfo),
		logger:  logger,
		timeout: timeout,
	}
}

// Register registers a new worker
func (r *Registry) Register(reg *pb.WorkerRegistration, authToken string) (*WorkerInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	workerID := generateWorkerID()

	worker := &WorkerInfo{
		ID:             workerID,
		Name:           reg.WorkerName,
		Hostname:       reg.Hostname,
		GRPCAddress:    reg.GrpcAddress,
		TLSFingerprint: reg.TlsFingerprint,
		Labels:         reg.Labels,
		Version:        reg.Version,
		Status:         pb.WorkerStatus_WORKER_STATUS_IDLE,
		AuthToken:      authToken,
		RegisteredAt:   time.Now(),
		LastHeartbeat:  time.Now(),
		Containers:     make([]*pb.ContainerResource, 0),
		Images:         make([]*pb.ImageResource, 0),
		Volumes:        make([]*pb.VolumeResource, 0),
		Networks:       make([]*pb.NetworkResource, 0),
	}

	r.workers[workerID] = worker

	r.logger.Info("worker registered",
		zap.String("worker_id", workerID),
		zap.String("name", reg.WorkerName),
		zap.String("hostname", reg.Hostname),
	)

	return worker, nil
}

// Unregister removes a worker
func (r *Registry) Unregister(workerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if w, ok := r.workers[workerID]; ok {
		r.logger.Info("worker unregistered",
			zap.String("worker_id", workerID),
			zap.String("name", w.Name),
		)
		delete(r.workers, workerID)
	}
}

// Get returns a worker by ID
func (r *Registry) Get(workerID string) (*WorkerInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.workers[workerID]
	return w, ok
}

// GetByAuthToken returns a worker by auth token
func (r *Registry) GetByAuthToken(token string) (*WorkerInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, w := range r.workers {
		if w.AuthToken == token {
			return w, true
		}
	}
	return nil, false
}

// List returns all workers
func (r *Registry) List() []*WorkerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	workers := make([]*WorkerInfo, 0, len(r.workers))
	for _, w := range r.workers {
		workers = append(workers, w)
	}
	return workers
}

// ListOnline returns only online workers
func (r *Registry) ListOnline() []*WorkerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	workers := make([]*WorkerInfo, 0)
	cutoff := time.Now().Add(-r.timeout)

	for _, w := range r.workers {
		if w.LastHeartbeat.After(cutoff) {
			workers = append(workers, w)
		}
	}
	return workers
}

// UpdateHeartbeat updates a worker's last heartbeat time
func (r *Registry) UpdateHeartbeat(workerID string, status pb.WorkerStatus, resources *pb.SystemResources) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if w, ok := r.workers[workerID]; ok {
		w.LastHeartbeat = time.Now()
		w.Status = status
		if resources != nil {
			w.SystemResources = resources
		}
	}
}

// UpdateInventory updates a worker's resource inventory
func (r *Registry) UpdateInventory(workerID string, inv *pb.ResourceInventory) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if w, ok := r.workers[workerID]; ok {
		w.LastInventory = time.Now()
		w.Containers = inv.Containers
		w.Images = inv.Images
		w.Volumes = inv.Volumes
		w.Networks = inv.Networks
	}
}

// SetStream sets the bidirectional stream for a worker
func (r *Registry) SetStream(workerID string, stream pb.MasterService_WorkerStreamServer) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if w, ok := r.workers[workerID]; ok {
		w.streamMu.Lock()
		w.stream = stream
		w.streamMu.Unlock()
	}
}

// SendCommand sends a command to a worker
func (r *Registry) SendCommand(workerID string, cmd *pb.MasterCommand) error {
	r.mu.RLock()
	w, ok := r.workers[workerID]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("worker not found: %s", workerID)
	}

	w.streamMu.Lock()
	defer w.streamMu.Unlock()

	if w.stream == nil {
		return fmt.Errorf("worker stream not connected: %s", workerID)
	}

	return w.stream.Send(cmd)
}

// StartCleanup periodically removes stale workers
func (r *Registry) StartCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.cleanupStale()
		}
	}
}

func (r *Registry) cleanupStale() {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-r.timeout * 3) // 3x timeout before removal

	for id, w := range r.workers {
		if w.LastHeartbeat.Before(cutoff) {
			r.logger.Warn("removing stale worker",
				zap.String("worker_id", id),
				zap.String("name", w.Name),
				zap.Duration("since_heartbeat", time.Since(w.LastHeartbeat)),
			)
			delete(r.workers, id)
		}
	}
}

// IsOnline checks if a worker is online
func (r *Registry) IsOnline(workerID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	w, ok := r.workers[workerID]
	if !ok {
		return false
	}
	return w.LastHeartbeat.After(time.Now().Add(-r.timeout))
}

func generateWorkerID() string {
	return fmt.Sprintf("worker-%d", time.Now().UnixNano())
}
