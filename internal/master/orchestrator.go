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

// MigrationJob represents a migration between two workers
type MigrationJob struct {
	ID             string
	SourceWorkerID string
	TargetWorkerID string

	ContainerIDs []string
	ImageIDs     []string
	VolumeNames  []string
	NetworkIDs   []string

	Mode     pb.MigrationMode
	Strategy pb.MigrationStrategy

	Status           MigrationJobStatus
	Phase            pb.MigrationPhase
	Progress         float32
	BytesTransferred int64
	TotalBytes       int64

	StartedAt   time.Time
	CompletedAt time.Time
	Error       string

	mu sync.RWMutex
}

// MigrationJobStatus represents the status of a migration job
type MigrationJobStatus string

const (
	MigrationStatusPending   MigrationJobStatus = "pending"
	MigrationStatusRunning   MigrationJobStatus = "running"
	MigrationStatusCompleted MigrationJobStatus = "completed"
	MigrationStatusFailed    MigrationJobStatus = "failed"
	MigrationStatusCancelled MigrationJobStatus = "cancelled"
)

// Orchestrator coordinates migrations between workers
type Orchestrator struct {
	registry *Registry
	logger   *observability.Logger

	migrations map[string]*MigrationJob
	mu         sync.RWMutex
}

// NewOrchestrator creates a new migration orchestrator
func NewOrchestrator(registry *Registry, logger *observability.Logger) *Orchestrator {
	return &Orchestrator{
		registry:   registry,
		logger:     logger,
		migrations: make(map[string]*MigrationJob),
	}
}

// StartMigration initiates a migration between two workers
func (o *Orchestrator) StartMigration(ctx context.Context, req *MigrationRequest) (*MigrationJob, error) {
	// Validate workers exist and are online
	source, ok := o.registry.Get(req.SourceWorkerID)
	if !ok {
		return nil, fmt.Errorf("source worker not found: %s", req.SourceWorkerID)
	}
	if !o.registry.IsOnline(req.SourceWorkerID) {
		return nil, fmt.Errorf("source worker is offline: %s", req.SourceWorkerID)
	}

	target, ok := o.registry.Get(req.TargetWorkerID)
	if !ok {
		return nil, fmt.Errorf("target worker not found: %s", req.TargetWorkerID)
	}
	if !o.registry.IsOnline(req.TargetWorkerID) {
		return nil, fmt.Errorf("target worker is offline: %s", req.TargetWorkerID)
	}

	// Create migration job
	job := &MigrationJob{
		ID:             generateMigrationID(),
		SourceWorkerID: req.SourceWorkerID,
		TargetWorkerID: req.TargetWorkerID,
		ContainerIDs:   req.ContainerIDs,
		ImageIDs:       req.ImageIDs,
		VolumeNames:    req.VolumeNames,
		NetworkIDs:     req.NetworkIDs,
		Mode:           req.Mode,
		Strategy:       req.Strategy,
		Status:         MigrationStatusPending,
		Phase:          pb.MigrationPhase_MIGRATION_PHASE_INITIALIZING,
		StartedAt:      time.Now(),
	}

	o.mu.Lock()
	o.migrations[job.ID] = job
	o.mu.Unlock()

	o.logger.Info("starting migration",
		zap.String("migration_id", job.ID),
		zap.String("source", source.Name),
		zap.String("target", target.Name),
	)

	// Start migration in background
	go o.executeMigration(ctx, job, source, target)

	return job, nil
}

func (o *Orchestrator) executeMigration(ctx context.Context, job *MigrationJob, source, target *WorkerInfo) {
	job.mu.Lock()
	job.Status = MigrationStatusRunning
	job.mu.Unlock()

	// Step 1: Tell target to prepare for incoming migration
	acceptCmd := &pb.MasterCommand{
		CommandId: fmt.Sprintf("accept-%s", job.ID),
		Payload: &pb.MasterCommand_StartMigration{
			StartMigration: &pb.StartMigrationCommand{
				Role: pb.MigrationRole_MIGRATION_ROLE_TARGET,
				AcceptRequest: &pb.AcceptMigrationRequest{
					MigrationId:       job.ID,
					SourceWorkerId:    job.SourceWorkerID,
					SourceAddress:     source.GRPCAddress,
					SourceFingerprint: source.TLSFingerprint,
					ContainerIds:      job.ContainerIDs,
					ImageIds:          job.ImageIDs,
					VolumeNames:       job.VolumeNames,
					NetworkIds:        job.NetworkIDs,
				},
			},
		},
	}

	if err := o.registry.SendCommand(job.TargetWorkerID, acceptCmd); err != nil {
		o.failMigration(job, fmt.Errorf("failed to notify target: %w", err))
		return
	}

	// Step 2: Tell source to start sending
	startCmd := &pb.MasterCommand{
		CommandId: fmt.Sprintf("start-%s", job.ID),
		Payload: &pb.MasterCommand_StartMigration{
			StartMigration: &pb.StartMigrationCommand{
				Role: pb.MigrationRole_MIGRATION_ROLE_SOURCE,
				Request: &pb.MigrationRequest{
					MigrationId:       job.ID,
					TargetWorkerId:    job.TargetWorkerID,
					TargetAddress:     target.GRPCAddress,
					TargetFingerprint: target.TLSFingerprint,
					ContainerIds:      job.ContainerIDs,
					ImageIds:          job.ImageIDs,
					VolumeNames:       job.VolumeNames,
					NetworkIds:        job.NetworkIDs,
					Mode:              job.Mode,
					Strategy:          job.Strategy,
				},
			},
		},
	}

	if err := o.registry.SendCommand(job.SourceWorkerID, startCmd); err != nil {
		o.failMigration(job, fmt.Errorf("failed to notify source: %w", err))
		return
	}

	o.logger.Info("migration commands sent",
		zap.String("migration_id", job.ID),
	)
}

func (o *Orchestrator) failMigration(job *MigrationJob, err error) {
	job.mu.Lock()
	job.Status = MigrationStatusFailed
	job.Phase = pb.MigrationPhase_MIGRATION_PHASE_FAILED
	job.Error = err.Error()
	job.CompletedAt = time.Now()
	job.mu.Unlock()

	o.logger.Error("migration failed",
		zap.String("migration_id", job.ID),
		zap.Error(err),
	)
}

// UpdateProgress updates migration progress from worker reports
func (o *Orchestrator) UpdateProgress(migrationID string, progress *pb.MigrationProgress) {
	o.mu.RLock()
	job, ok := o.migrations[migrationID]
	o.mu.RUnlock()

	if !ok {
		return
	}

	job.mu.Lock()
	job.Phase = progress.Phase
	job.Progress = progress.Progress
	job.BytesTransferred = progress.BytesTransferred
	job.TotalBytes = progress.TotalBytes
	job.mu.Unlock()
}

// CompleteMigration marks a migration as complete
func (o *Orchestrator) CompleteMigration(migrationID string, complete *pb.MigrationComplete) {
	o.mu.RLock()
	job, ok := o.migrations[migrationID]
	o.mu.RUnlock()

	if !ok {
		return
	}

	job.mu.Lock()
	if complete.Success {
		job.Status = MigrationStatusCompleted
		job.Phase = pb.MigrationPhase_MIGRATION_PHASE_COMPLETE
	} else {
		job.Status = MigrationStatusFailed
		job.Phase = pb.MigrationPhase_MIGRATION_PHASE_FAILED
		job.Error = complete.Error
	}
	job.CompletedAt = time.Now()
	job.BytesTransferred = complete.BytesTransferred
	job.mu.Unlock()

	o.logger.Info("migration completed",
		zap.String("migration_id", migrationID),
		zap.Bool("success", complete.Success),
	)
}

// CancelMigration cancels a running migration
func (o *Orchestrator) CancelMigration(migrationID string, reason string) error {
	o.mu.RLock()
	job, ok := o.migrations[migrationID]
	o.mu.RUnlock()

	if !ok {
		return fmt.Errorf("migration not found: %s", migrationID)
	}

	job.mu.Lock()
	if job.Status != MigrationStatusRunning && job.Status != MigrationStatusPending {
		job.mu.Unlock()
		return fmt.Errorf("migration cannot be cancelled in state: %s", job.Status)
	}
	job.Status = MigrationStatusCancelled
	job.Phase = pb.MigrationPhase_MIGRATION_PHASE_CANCELLED
	job.Error = reason
	job.CompletedAt = time.Now()
	job.mu.Unlock()

	// Send cancel commands to both workers
	cancelCmd := &pb.MasterCommand{
		CommandId: fmt.Sprintf("cancel-%s", migrationID),
		Payload: &pb.MasterCommand_CancelMigration{
			CancelMigration: &pb.CancelMigrationCommand{
				MigrationId: migrationID,
				Reason:      reason,
			},
		},
	}

	// Best effort - don't fail if we can't reach workers
	_ = o.registry.SendCommand(job.SourceWorkerID, cancelCmd)
	_ = o.registry.SendCommand(job.TargetWorkerID, cancelCmd)

	return nil
}

// GetMigration returns a migration by ID
func (o *Orchestrator) GetMigration(migrationID string) (*MigrationJob, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	job, ok := o.migrations[migrationID]
	return job, ok
}

// ListMigrations returns all migrations
func (o *Orchestrator) ListMigrations() []*MigrationJob {
	o.mu.RLock()
	defer o.mu.RUnlock()

	jobs := make([]*MigrationJob, 0, len(o.migrations))
	for _, j := range o.migrations {
		jobs = append(jobs, j)
	}
	return jobs
}

// MigrationRequest is the input for starting a migration
type MigrationRequest struct {
	SourceWorkerID string
	TargetWorkerID string
	ContainerIDs   []string
	ImageIDs       []string
	VolumeNames    []string
	NetworkIDs     []string
	Mode           pb.MigrationMode
	Strategy       pb.MigrationStrategy
}

func generateMigrationID() string {
	return fmt.Sprintf("mig-%d", time.Now().UnixNano())
}
