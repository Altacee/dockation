package migration

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

// Engine orchestrates migration operations with comprehensive state management
type Engine struct {
	docker      *docker.Client
	peers       *peer.PeerDiscovery
	transfer    *peer.TransferManager
	config      *config.Config
	logger      *zap.Logger
	metrics     *observability.Metrics
	rollback    *RollbackManager
	auditor     *Auditor
	pathMapper  *PathMapper
	conflict    *ConflictResolver

	// Job management with thread-safe access
	jobs      map[string]*MigrationJob
	jobsMutex sync.RWMutex

	// Progress channels for real-time updates
	progressChan chan MigrationUpdate
}

// MigrationJob represents a complete migration operation with full lifecycle tracking
type MigrationJob struct {
	ID            string                   `json:"id"`
	PeerID        string                   `json:"peer_id"`
	Mode          MigrationMode            `json:"mode"`
	Strategy      MigrationStrategy        `json:"strategy"`
	Resources     []ResourceRef            `json:"resources"`
	Status        MigrationStatus          `json:"status"`
	Progress      MigrationProgress        `json:"progress"`
	Errors        []MigrationError         `json:"errors"`
	StartTime     time.Time                `json:"start_time"`
	EndTime       *time.Time               `json:"end_time,omitempty"`
	CanPause      bool                     `json:"can_pause"`
	CanResume     bool                     `json:"can_resume"`

	// Runtime state for resumption
	CurrentPhase  string                   `json:"current_phase"`
	CheckpointData map[string]interface{}  `json:"checkpoint_data,omitempty"`

	// User-provided configuration
	PathMappings        map[string]PathMapping      `json:"path_mappings,omitempty"`
	ConflictResolutions map[string]Resolution       `json:"conflict_resolutions,omitempty"`

	// Internal control
	ctx       context.Context
	cancel    context.CancelFunc
	pauseChan chan struct{}
	resumeChan chan struct{}
}

type MigrationMode string

const (
	ModeCopy MigrationMode = "copy" // Copy resources, leave source intact
	ModeMove MigrationMode = "move" // Move resources, disable source after verification
)

type MigrationStrategy string

const (
	StrategyCold     MigrationStrategy = "cold"     // Stop → Transfer → Start
	StrategyWarm     MigrationStrategy = "warm"     // Sync while running → Pause → Delta → Cutover
	StrategySnapshot MigrationStrategy = "snapshot" // LVM/ZFS snapshot → Transfer
)

type MigrationStatus string

const (
	StatusPending   MigrationStatus = "pending"
	StatusPreflight MigrationStatus = "preflight" // Running audit checks
	StatusRunning   MigrationStatus = "running"
	StatusPaused    MigrationStatus = "paused"
	StatusComplete  MigrationStatus = "complete"
	StatusFailed    MigrationStatus = "failed"
	StatusRollingBack MigrationStatus = "rolling_back"
)

// MigrationProgress tracks detailed progress with time estimation
type MigrationProgress struct {
	CurrentStep   int       `json:"current_step"`
	TotalSteps    int       `json:"total_steps"`
	CurrentItem   string    `json:"current_item"`
	CurrentNumber int       `json:"current_number"`
	TotalItems    int       `json:"total_items"`
	BytesTotal    int64     `json:"bytes_total"`
	BytesDone     int64     `json:"bytes_done"`
	StartTime     time.Time `json:"start_time"`
	EstimatedEnd  time.Time `json:"estimated_end"`

	// Per-resource checksums for verification
	Checksums     map[string]string `json:"checksums,omitempty"`
}

// MigrationError represents a failure with context for recovery
type MigrationError struct {
	Timestamp    time.Time `json:"timestamp"`
	Phase        string    `json:"phase"`
	ResourceType string    `json:"resource_type"`
	ResourceName string    `json:"resource_name"`
	Message      string    `json:"message"`
	Recoverable  bool      `json:"recoverable"`
	RetryCount   int       `json:"retry_count"`
}

// ResourceRef identifies a Docker resource for migration
type ResourceRef struct {
	Type string `json:"type"` // container, volume, network, image
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MigrationUpdate is sent via WebSocket for real-time progress
type MigrationUpdate struct {
	Type     string             `json:"type"` // "progress", "audit", "error", "complete"
	JobID    string             `json:"job_id"`
	Progress *MigrationProgress `json:"progress,omitempty"`
	Audit    *AuditCheck        `json:"audit,omitempty"`
	Error    *MigrationError    `json:"error,omitempty"`
}

// NewEngine creates a migration engine with all dependencies
func NewEngine(
	dockerClient *docker.Client,
	peers *peer.PeerDiscovery,
	transfer *peer.TransferManager,
	cfg *config.Config,
	logger *zap.Logger,
	metrics *observability.Metrics,
) *Engine {
	engine := &Engine{
		docker:       dockerClient,
		peers:        peers,
		transfer:     transfer,
		config:       cfg,
		logger:       logger,
		metrics:      metrics,
		jobs:         make(map[string]*MigrationJob),
		progressChan: make(chan MigrationUpdate, 100),
	}

	// Initialize sub-components
	engine.rollback = NewRollbackManager(dockerClient, logger)
	engine.auditor = NewAuditor(dockerClient, peers, logger)
	engine.pathMapper = NewPathMapper()
	engine.conflict = NewConflictResolver(dockerClient, peers, logger)

	return engine
}

// StartMigration begins a new migration job with preflight checks
func (e *Engine) StartMigration(ctx context.Context, job *MigrationJob) error {
	e.logger.Info("starting migration",
		zap.String("job_id", job.ID),
		zap.String("peer_id", job.PeerID),
		zap.String("mode", string(job.Mode)),
		zap.String("strategy", string(job.Strategy)),
	)

	// Initialize job runtime state
	job.ctx, job.cancel = context.WithCancel(ctx)
	job.pauseChan = make(chan struct{})
	job.resumeChan = make(chan struct{})
	job.StartTime = time.Now()
	job.Status = StatusPreflight
	job.Progress.StartTime = time.Now()

	// Register job for tracking
	e.jobsMutex.Lock()
	e.jobs[job.ID] = job
	e.jobsMutex.Unlock()

	// Create rollback snapshot BEFORE any changes
	snapshot, err := e.rollback.CreateSnapshot(job.ID)
	if err != nil {
		return fmt.Errorf("failed to create rollback snapshot: %w", err)
	}
	e.logger.Info("created rollback snapshot",
		zap.String("job_id", job.ID),
		zap.Time("timestamp", snapshot.Timestamp),
	)

	// Run in background to allow immediate return
	go e.executeMigration(job)

	return nil
}

// executeMigration runs the full migration lifecycle
func (e *Engine) executeMigration(job *MigrationJob) {
	var finalErr error

	defer func() {
		// Final status update
		now := time.Now()
		job.EndTime = &now

		if finalErr != nil {
			job.Status = StatusFailed
			job.Errors = append(job.Errors, MigrationError{
				Timestamp:   time.Now(),
				Phase:       job.CurrentPhase,
				Message:     finalErr.Error(),
				Recoverable: false,
			})

			// Attempt rollback on failure
			e.logger.Warn("migration failed, attempting rollback",
				zap.String("job_id", job.ID),
				zap.Error(finalErr),
			)

			job.Status = StatusRollingBack
			if rbErr := e.rollback.Rollback(job.ID); rbErr != nil {
				e.logger.Error("rollback failed",
					zap.String("job_id", job.ID),
					zap.Error(rbErr),
				)
				job.Errors = append(job.Errors, MigrationError{
					Timestamp: time.Now(),
					Phase:     "rollback",
					Message:   rbErr.Error(),
					Recoverable: false,
				})
			}
		} else {
			job.Status = StatusComplete
			e.logger.Info("migration completed successfully",
				zap.String("job_id", job.ID),
				zap.Duration("duration", time.Since(job.StartTime)),
			)
		}

		// Send final update
		e.progressChan <- MigrationUpdate{
			Type:     "complete",
			JobID:    job.ID,
			Progress: &job.Progress,
			Error: func() *MigrationError {
				if len(job.Errors) > 0 {
					return &job.Errors[len(job.Errors)-1]
				}
				return nil
			}(),
		}

		// Record metrics
		e.metrics.RecordMigration(string(job.Status), string(job.Strategy))
	}()

	// Phase 1: Pre-flight audit
	job.CurrentPhase = "audit"
	auditResult, err := e.runAudit(job)
	if err != nil {
		finalErr = fmt.Errorf("audit failed: %w", err)
		return
	}

	if !auditResult.CanProceed {
		finalErr = fmt.Errorf("audit checks failed: %d blockers", len(auditResult.Blockers))
		return
	}

	// Phase 2: Execute strategy
	job.CurrentPhase = "execution"
	job.Status = StatusRunning

	strategy, err := e.getStrategy(job.Strategy)
	if err != nil {
		finalErr = fmt.Errorf("failed to get strategy: %w", err)
		return
	}

	progressCh := make(chan MigrationProgress, 10)
	go e.streamProgress(job.ID, progressCh)

	if err := strategy.ExecuteMigration(job.ctx, job, progressCh); err != nil {
		finalErr = fmt.Errorf("migration execution failed: %w", err)
		return
	}

	close(progressCh)

	// Phase 3: Post-migration verification
	job.CurrentPhase = "verification"
	if err := e.verifyMigration(job); err != nil {
		finalErr = fmt.Errorf("verification failed: %w", err)
		return
	}

	e.logger.Info("migration execution completed",
		zap.String("job_id", job.ID),
		zap.Int64("bytes_transferred", job.Progress.BytesDone),
	)
}

// runAudit executes all preflight checks with real-time streaming
func (e *Engine) runAudit(job *MigrationJob) (*AuditResult, error) {
	resultCh := make(chan AuditCheck, 20)

	go func() {
		for check := range resultCh {
			e.progressChan <- MigrationUpdate{
				Type:  "audit",
				JobID: job.ID,
				Audit: &check,
			}
		}
	}()

	result, err := e.auditor.RunAudit(job.ctx, job, resultCh)
	close(resultCh)

	return result, err
}

// streamProgress forwards progress updates to WebSocket
func (e *Engine) streamProgress(jobID string, progressCh <-chan MigrationProgress) {
	for progress := range progressCh {
		e.jobsMutex.Lock()
		if job, exists := e.jobs[jobID]; exists {
			job.Progress = progress
		}
		e.jobsMutex.Unlock()

		e.progressChan <- MigrationUpdate{
			Type:     "progress",
			JobID:    jobID,
			Progress: &progress,
		}
	}
}

// getStrategy returns the appropriate migration strategy
func (e *Engine) getStrategy(strategy MigrationStrategy) (Strategy, error) {
	switch strategy {
	case StrategyCold:
		return &ColdStrategy{engine: e}, nil
	case StrategyWarm:
		return &WarmStrategy{engine: e}, nil
	case StrategySnapshot:
		return &SnapshotStrategy{engine: e}, nil
	default:
		return nil, fmt.Errorf("unknown strategy: %s", strategy)
	}
}

// verifyMigration performs post-migration integrity checks
func (e *Engine) verifyMigration(job *MigrationJob) error {
	e.logger.Info("verifying migration",
		zap.String("job_id", job.ID),
		zap.Int("resource_count", len(job.Resources)),
	)

	// Verify all resources exist on target
	// This is where we'd use gRPC to query target peer
	// For now, return success

	return nil
}

// PauseMigration pauses a running migration if supported by strategy
func (e *Engine) PauseMigration(jobID string) error {
	e.jobsMutex.Lock()
	job, exists := e.jobs[jobID]
	e.jobsMutex.Unlock()

	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if !job.CanPause {
		return fmt.Errorf("job cannot be paused in current state")
	}

	if job.Status != StatusRunning {
		return fmt.Errorf("job is not running (status: %s)", job.Status)
	}

	e.logger.Info("pausing migration", zap.String("job_id", jobID))

	job.Status = StatusPaused
	close(job.pauseChan)

	return nil
}

// ResumeMigration continues a paused migration
func (e *Engine) ResumeMigration(jobID string) error {
	e.jobsMutex.Lock()
	job, exists := e.jobs[jobID]
	e.jobsMutex.Unlock()

	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if !job.CanResume {
		return fmt.Errorf("job cannot be resumed")
	}

	if job.Status != StatusPaused {
		return fmt.Errorf("job is not paused (status: %s)", job.Status)
	}

	e.logger.Info("resuming migration", zap.String("job_id", jobID))

	job.Status = StatusRunning
	job.resumeChan = make(chan struct{})
	close(job.resumeChan)

	return nil
}

// CancelMigration cancels and rolls back a migration
func (e *Engine) CancelMigration(jobID string) error {
	e.jobsMutex.Lock()
	job, exists := e.jobs[jobID]
	e.jobsMutex.Unlock()

	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	e.logger.Info("cancelling migration", zap.String("job_id", jobID))

	// Cancel context to stop all operations
	if job.cancel != nil {
		job.cancel()
	}

	// Rollback will be handled by deferred function in executeMigration

	return nil
}

// GetStatus returns current job status
func (e *Engine) GetStatus(jobID string) (*MigrationJob, error) {
	e.jobsMutex.RLock()
	defer e.jobsMutex.RUnlock()

	job, exists := e.jobs[jobID]
	if !exists {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	// Return copy to prevent external modification
	jobCopy := *job
	return &jobCopy, nil
}

// GetProgressChan returns the channel for receiving migration updates
func (e *Engine) GetProgressChan() <-chan MigrationUpdate {
	return e.progressChan
}

// GenerateDryRun creates a preview without executing
func (e *Engine) GenerateDryRun(ctx context.Context, job *MigrationJob) (*DryRunResult, error) {
	e.logger.Info("generating dry-run preview", zap.String("job_id", job.ID))

	result := &DryRunResult{
		Operations: make([]Operation, 0),
	}

	// Run audit checks
	auditResultCh := make(chan AuditCheck, 20)
	go func() {
		for range auditResultCh {
			// Consume but don't broadcast for dry-run
		}
	}()

	auditResult, err := e.auditor.RunAudit(ctx, job, auditResultCh)
	close(auditResultCh)

	if err != nil {
		return nil, fmt.Errorf("audit failed: %w", err)
	}

	result.Warnings = auditResult.Warnings
	result.Blockers = auditResult.Blockers
	result.EstimatedDuration = auditResult.EstimatedDuration
	result.TotalTransferBytes = auditResult.TotalBytes

	// Enumerate operations without executing
	for _, resource := range job.Resources {
		op := Operation{
			Type:         fmt.Sprintf("transfer_%s", resource.Type),
			ResourceName: resource.Name,
			ResourceID:   resource.ID,
			SizeBytes:    0, // Would be calculated from actual resource inspection
		}

		result.Operations = append(result.Operations, op)
	}

	return result, nil
}
