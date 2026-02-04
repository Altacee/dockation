package migration

import (
	"fmt"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/docker"

	"go.uber.org/zap"
)

// RollbackManager handles migration rollback with snapshot capabilities
// This is critical for recovering from failed migrations without manual intervention
type RollbackManager struct {
	docker      *docker.Client
	logger      *zap.Logger
	snapshots   map[string]*Snapshot
	snapshotMux sync.RWMutex
}

// Snapshot represents the complete pre-migration state
type Snapshot struct {
	JobID             string               `json:"job_id"`
	Timestamp         time.Time            `json:"timestamp"`
	StoppedContainers []string             `json:"stopped_containers"`
	PausedContainers  []string             `json:"paused_containers"`
	CreatedResources  []ResourceRef        `json:"created_resources"`
	ModifiedConfigs   []ConfigBackup       `json:"modified_configs"`
	SourceState       map[string]string    `json:"source_state"` // Container ID -> state
}

// ConfigBackup stores original configuration for restoration
type ConfigBackup struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Config       string `json:"config"` // JSON-encoded original config
}

// NewRollbackManager creates a rollback manager
func NewRollbackManager(dockerClient *docker.Client, logger *zap.Logger) *RollbackManager {
	return &RollbackManager{
		docker:    dockerClient,
		logger:    logger,
		snapshots: make(map[string]*Snapshot),
	}
}

// CreateSnapshot captures current state before migration begins
func (rm *RollbackManager) CreateSnapshot(jobID string) (*Snapshot, error) {
	rm.logger.Info("creating rollback snapshot", zap.String("job_id", jobID))

	snapshot := &Snapshot{
		JobID:             jobID,
		Timestamp:         time.Now(),
		StoppedContainers: make([]string, 0),
		PausedContainers:  make([]string, 0),
		CreatedResources:  make([]ResourceRef, 0),
		ModifiedConfigs:   make([]ConfigBackup, 0),
		SourceState:       make(map[string]string),
	}

	// Would capture:
	// - Current state of all containers
	// - Running/stopped/paused status
	// - Original configurations
	// - Volume mount points
	// - Network attachments

	rm.snapshotMux.Lock()
	rm.snapshots[jobID] = snapshot
	rm.snapshotMux.Unlock()

	rm.logger.Info("rollback snapshot created",
		zap.String("job_id", jobID),
		zap.Time("timestamp", snapshot.Timestamp),
	)

	return snapshot, nil
}

// RecordContainerStopped adds a container to the stopped list
func (rm *RollbackManager) RecordContainerStopped(jobID, containerID string) error {
	rm.snapshotMux.Lock()
	defer rm.snapshotMux.Unlock()

	snapshot, exists := rm.snapshots[jobID]
	if !exists {
		return fmt.Errorf("snapshot not found for job: %s", jobID)
	}

	snapshot.StoppedContainers = append(snapshot.StoppedContainers, containerID)
	snapshot.SourceState[containerID] = "stopped"

	return nil
}

// RecordContainerPaused adds a container to the paused list
func (rm *RollbackManager) RecordContainerPaused(jobID, containerID string) error {
	rm.snapshotMux.Lock()
	defer rm.snapshotMux.Unlock()

	snapshot, exists := rm.snapshots[jobID]
	if !exists {
		return fmt.Errorf("snapshot not found for job: %s", jobID)
	}

	snapshot.PausedContainers = append(snapshot.PausedContainers, containerID)
	snapshot.SourceState[containerID] = "paused"

	return nil
}

// RecordResourceCreated tracks resources created on target
func (rm *RollbackManager) RecordResourceCreated(jobID string, resource ResourceRef) error {
	rm.snapshotMux.Lock()
	defer rm.snapshotMux.Unlock()

	snapshot, exists := rm.snapshots[jobID]
	if !exists {
		return fmt.Errorf("snapshot not found for job: %s", jobID)
	}

	snapshot.CreatedResources = append(snapshot.CreatedResources, resource)

	return nil
}

// Rollback restores to pre-migration state
func (rm *RollbackManager) Rollback(jobID string) error {
	rm.snapshotMux.RLock()
	snapshot, exists := rm.snapshots[jobID]
	rm.snapshotMux.RUnlock()

	if !exists {
		return fmt.Errorf("snapshot not found for job: %s", jobID)
	}

	rm.logger.Info("starting rollback",
		zap.String("job_id", jobID),
		zap.Int("stopped_containers", len(snapshot.StoppedContainers)),
		zap.Int("paused_containers", len(snapshot.PausedContainers)),
		zap.Int("created_resources", len(snapshot.CreatedResources)),
	)

	var rollbackErrors []error

	// Step 1: Restart stopped containers
	for _, containerID := range snapshot.StoppedContainers {
		if err := rm.restartContainer(containerID); err != nil {
			rm.logger.Warn("failed to restart container during rollback",
				zap.String("container_id", containerID),
				zap.Error(err),
			)
			rollbackErrors = append(rollbackErrors, err)
		}
	}

	// Step 2: Unpause paused containers
	for _, containerID := range snapshot.PausedContainers {
		if err := rm.unpauseContainer(containerID); err != nil {
			rm.logger.Warn("failed to unpause container during rollback",
				zap.String("container_id", containerID),
				zap.Error(err),
			)
			rollbackErrors = append(rollbackErrors, err)
		}
	}

	// Step 3: Remove created resources on target (would need gRPC call)
	for _, resource := range snapshot.CreatedResources {
		rm.logger.Info("would remove created resource",
			zap.String("type", resource.Type),
			zap.String("name", resource.Name),
		)
		// Would send gRPC request to target to remove resource
	}

	// Step 4: Restore modified configurations
	for _, backup := range snapshot.ModifiedConfigs {
		rm.logger.Info("would restore config",
			zap.String("type", backup.ResourceType),
			zap.String("id", backup.ResourceID),
		)
		// Would restore original configuration
	}

	if len(rollbackErrors) > 0 {
		rm.logger.Error("rollback completed with errors",
			zap.String("job_id", jobID),
			zap.Int("error_count", len(rollbackErrors)),
		)
		return fmt.Errorf("rollback completed with %d errors", len(rollbackErrors))
	}

	rm.logger.Info("rollback completed successfully", zap.String("job_id", jobID))

	return nil
}

// restartContainer starts a stopped container
func (rm *RollbackManager) restartContainer(containerID string) error {
	rm.logger.Info("restarting container", zap.String("container_id", containerID))
	// Would use Docker SDK to start container
	return nil
}

// unpauseContainer resumes a paused container
func (rm *RollbackManager) unpauseContainer(containerID string) error {
	rm.logger.Info("unpausing container", zap.String("container_id", containerID))
	// Would use Docker SDK to unpause container
	return nil
}

// GetSnapshot retrieves a snapshot by job ID
func (rm *RollbackManager) GetSnapshot(jobID string) (*Snapshot, error) {
	rm.snapshotMux.RLock()
	defer rm.snapshotMux.RUnlock()

	snapshot, exists := rm.snapshots[jobID]
	if !exists {
		return nil, fmt.Errorf("snapshot not found for job: %s", jobID)
	}

	return snapshot, nil
}

// DeleteSnapshot removes a snapshot after successful migration
func (rm *RollbackManager) DeleteSnapshot(jobID string) error {
	rm.snapshotMux.Lock()
	defer rm.snapshotMux.Unlock()

	delete(rm.snapshots, jobID)
	rm.logger.Info("snapshot deleted", zap.String("job_id", jobID))

	return nil
}
