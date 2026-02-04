package migration

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// Strategy defines the interface for migration strategies
// Each strategy implements a different approach to minimize downtime and ensure data integrity
type Strategy interface {
	// PrepareMigration validates and prepares for migration execution
	PrepareMigration(ctx context.Context, job *MigrationJob) error

	// ExecuteMigration runs the actual migration with progress reporting
	ExecuteMigration(ctx context.Context, job *MigrationJob, progressCh chan<- MigrationProgress) error

	// Rollback reverts any changes made during migration
	Rollback(ctx context.Context, job *MigrationJob) error
}

// ColdStrategy implements Stop → Transfer → Start migration
// This is the safest strategy with maximum downtime but guaranteed consistency
type ColdStrategy struct {
	engine *Engine
}

func (s *ColdStrategy) PrepareMigration(ctx context.Context, job *MigrationJob) error {
	s.engine.logger.Info("preparing cold migration",
		zap.String("job_id", job.ID),
		zap.Int("resource_count", len(job.Resources)),
	)

	// Cold migration can always be paused/resumed
	job.CanPause = true
	job.CanResume = true

	return nil
}

func (s *ColdStrategy) ExecuteMigration(ctx context.Context, job *MigrationJob, progressCh chan<- MigrationProgress) error {
	s.engine.logger.Info("executing cold migration", zap.String("job_id", job.ID))

	// Calculate total steps: containers, volumes, networks, images
	totalSteps := 0
	containerCount := 0
	volumeCount := 0
	networkCount := 0
	imageCount := 0

	for _, res := range job.Resources {
		switch res.Type {
		case "container":
			containerCount++
		case "volume":
			volumeCount++
		case "network":
			networkCount++
		case "image":
			imageCount++
		}
	}

	// Step allocation: stop, images, volumes, networks, containers, cleanup
	totalSteps = 1 + imageCount + volumeCount + networkCount + containerCount + 1
	currentStep := 0

	progress := MigrationProgress{
		TotalSteps:  totalSteps,
		TotalItems:  len(job.Resources),
		Checksums:   make(map[string]string),
		StartTime:   time.Now(),
	}

	// Step 1: Stop source containers
	currentStep++
	progress.CurrentStep = currentStep
	progress.CurrentItem = "Stopping source containers"
	progressCh <- progress

	for _, res := range job.Resources {
		if res.Type == "container" {
			if err := s.stopContainer(ctx, res.Name); err != nil {
				return fmt.Errorf("failed to stop container %s: %w", res.Name, err)
			}
		}
	}

	// Step 2: Migrate images with layer deduplication
	imageMigrator := &ImageMigrator{
		docker:   s.engine.docker,
		transfer: s.engine.transfer,
		logger:   s.engine.logger,
	}

	for i, res := range job.Resources {
		if res.Type == "image" {
			currentStep++
			progress.CurrentStep = currentStep
			progress.CurrentNumber = i + 1
			progress.CurrentItem = fmt.Sprintf("Transferring image: %s", res.Name)
			progressCh <- progress

			if err := imageMigrator.MigrateImage(ctx, res.ID, job.PeerID, progressCh); err != nil {
				return fmt.Errorf("failed to migrate image %s: %w", res.Name, err)
			}
		}
	}

	// Step 3: Migrate volumes with checksums
	volumeMigrator := &VolumeMigrator{
		docker:   s.engine.docker,
		transfer: s.engine.transfer,
		logger:   s.engine.logger,
	}

	for i, res := range job.Resources {
		if res.Type == "volume" {
			currentStep++
			progress.CurrentStep = currentStep
			progress.CurrentNumber = i + 1
			progress.CurrentItem = fmt.Sprintf("Transferring volume: %s", res.Name)
			progressCh <- progress

			if err := volumeMigrator.MigrateVolume(ctx, res.Name, job.PeerID, StrategyCold, progressCh); err != nil {
				return fmt.Errorf("failed to migrate volume %s: %w", res.Name, err)
			}
		}
	}

	// Step 4: Create networks on target
	networkMigrator := &NetworkMigrator{
		docker:   s.engine.docker,
		transfer: s.engine.transfer,
		logger:   s.engine.logger,
	}

	for i, res := range job.Resources {
		if res.Type == "network" {
			currentStep++
			progress.CurrentStep = currentStep
			progress.CurrentNumber = i + 1
			progress.CurrentItem = fmt.Sprintf("Creating network: %s", res.Name)
			progressCh <- progress

			if err := networkMigrator.MigrateNetwork(ctx, res.Name, job.PeerID); err != nil {
				return fmt.Errorf("failed to migrate network %s: %w", res.Name, err)
			}
		}
	}

	// Step 5: Create and start containers on target
	containerMigrator := &ContainerMigrator{
		docker:   s.engine.docker,
		transfer: s.engine.transfer,
		logger:   s.engine.logger,
	}

	for i, res := range job.Resources {
		if res.Type == "container" {
			currentStep++
			progress.CurrentStep = currentStep
			progress.CurrentNumber = i + 1
			progress.CurrentItem = fmt.Sprintf("Creating container: %s", res.Name)
			progressCh <- progress

			if err := containerMigrator.MigrateContainer(ctx, res.ID, job.PeerID, job.Mode, progressCh); err != nil {
				return fmt.Errorf("failed to migrate container %s: %w", res.Name, err)
			}
		}
	}

	// Step 6: Cleanup based on mode
	currentStep++
	progress.CurrentStep = currentStep
	progress.CurrentItem = "Finalizing migration"
	progressCh <- progress

	if job.Mode == ModeMove {
		// Disable source containers (rename with backup suffix)
		for _, res := range job.Resources {
			if res.Type == "container" {
				if err := s.disableSourceContainer(ctx, res.Name); err != nil {
					s.engine.logger.Warn("failed to disable source container",
						zap.String("container", res.Name),
						zap.Error(err),
					)
				}
			}
		}
	}

	progress.CurrentStep = totalSteps
	progress.EstimatedEnd = time.Now()
	progressCh <- progress

	return nil
}

func (s *ColdStrategy) Rollback(ctx context.Context, job *MigrationJob) error {
	s.engine.logger.Info("rolling back cold migration", zap.String("job_id", job.ID))

	// Restart stopped containers
	for _, res := range job.Resources {
		if res.Type == "container" {
			if err := s.startContainer(ctx, res.Name); err != nil {
				s.engine.logger.Warn("failed to restart container during rollback",
					zap.String("container", res.Name),
					zap.Error(err),
				)
			}
		}
	}

	return nil
}

func (s *ColdStrategy) stopContainer(ctx context.Context, name string) error {
	// Would use Docker SDK to stop container
	// For now, log the operation
	s.engine.logger.Info("stopping container", zap.String("name", name))
	return nil
}

func (s *ColdStrategy) startContainer(ctx context.Context, name string) error {
	s.engine.logger.Info("starting container", zap.String("name", name))
	return nil
}

func (s *ColdStrategy) disableSourceContainer(ctx context.Context, name string) error {
	// Rename container with backup suffix and disable restart policy
	s.engine.logger.Info("disabling source container",
		zap.String("name", name),
		zap.String("new_name", name+"-migrated-backup"),
	)
	return nil
}

// WarmStrategy implements Sync → Pause → Delta → Cutover migration
// This minimizes downtime by pre-syncing data while containers run
type WarmStrategy struct {
	engine *Engine
}

func (w *WarmStrategy) PrepareMigration(ctx context.Context, job *MigrationJob) error {
	w.engine.logger.Info("preparing warm migration",
		zap.String("job_id", job.ID),
	)

	// Warm migration supports pause/resume
	job.CanPause = true
	job.CanResume = true

	return nil
}

func (w *WarmStrategy) ExecuteMigration(ctx context.Context, job *MigrationJob, progressCh chan<- MigrationProgress) error {
	w.engine.logger.Info("executing warm migration", zap.String("job_id", job.ID))

	progress := MigrationProgress{
		TotalSteps:  5,
		TotalItems:  len(job.Resources),
		Checksums:   make(map[string]string),
		StartTime:   time.Now(),
	}

	// Phase 1: Initial warm sync while containers run
	progress.CurrentStep = 1
	progress.CurrentItem = "Initial warm sync (containers running)"
	progressCh <- progress

	volumeMigrator := &VolumeMigrator{
		docker:   w.engine.docker,
		transfer: w.engine.transfer,
		logger:   w.engine.logger,
	}

	for _, res := range job.Resources {
		if res.Type == "volume" {
			if err := volumeMigrator.warmSync(ctx, res.Name, job.PeerID, false); err != nil {
				return fmt.Errorf("warm sync failed for volume %s: %w", res.Name, err)
			}
		}
	}

	// Phase 2: Pause source containers
	progress.CurrentStep = 2
	progress.CurrentItem = "Pausing source containers"
	progressCh <- progress

	for _, res := range job.Resources {
		if res.Type == "container" {
			if err := w.pauseContainer(ctx, res.Name); err != nil {
				return fmt.Errorf("failed to pause container %s: %w", res.Name, err)
			}
		}
	}

	// Phase 3: Delta sync - only changes since initial sync
	progress.CurrentStep = 3
	progress.CurrentItem = "Delta sync (final changes)"
	progressCh <- progress

	for _, res := range job.Resources {
		if res.Type == "volume" {
			if err := volumeMigrator.warmSync(ctx, res.Name, job.PeerID, true); err != nil {
				return fmt.Errorf("delta sync failed for volume %s: %w", res.Name, err)
			}
		}
	}

	// Phase 4: Cutover - start on target
	progress.CurrentStep = 4
	progress.CurrentItem = "Starting containers on target"
	progressCh <- progress

	containerMigrator := &ContainerMigrator{
		docker:   w.engine.docker,
		transfer: w.engine.transfer,
		logger:   w.engine.logger,
	}

	for _, res := range job.Resources {
		if res.Type == "container" {
			if err := containerMigrator.MigrateContainer(ctx, res.ID, job.PeerID, job.Mode, progressCh); err != nil {
				return fmt.Errorf("failed to start container %s on target: %w", res.Name, err)
			}
		}
	}

	// Phase 5: Cleanup source
	progress.CurrentStep = 5
	progress.CurrentItem = "Cleanup"
	progressCh <- progress

	if job.Mode == ModeMove {
		for _, res := range job.Resources {
			if res.Type == "container" {
				if err := w.stopContainer(ctx, res.Name); err != nil {
					w.engine.logger.Warn("failed to stop source container",
						zap.String("container", res.Name),
						zap.Error(err),
					)
				}
			}
		}
	}

	progress.EstimatedEnd = time.Now()
	progressCh <- progress

	return nil
}

func (w *WarmStrategy) Rollback(ctx context.Context, job *MigrationJob) error {
	w.engine.logger.Info("rolling back warm migration", zap.String("job_id", job.ID))

	// Unpause containers
	for _, res := range job.Resources {
		if res.Type == "container" {
			if err := w.unpauseContainer(ctx, res.Name); err != nil {
				w.engine.logger.Warn("failed to unpause container during rollback",
					zap.String("container", res.Name),
					zap.Error(err),
				)
			}
		}
	}

	return nil
}

func (w *WarmStrategy) pauseContainer(ctx context.Context, name string) error {
	w.engine.logger.Info("pausing container", zap.String("name", name))
	return nil
}

func (w *WarmStrategy) unpauseContainer(ctx context.Context, name string) error {
	w.engine.logger.Info("unpausing container", zap.String("name", name))
	return nil
}

func (w *WarmStrategy) stopContainer(ctx context.Context, name string) error {
	w.engine.logger.Info("stopping container", zap.String("name", name))
	return nil
}

// SnapshotStrategy uses filesystem snapshots for instant migration
// Requires LVM/ZFS support on the host
type SnapshotStrategy struct {
	engine *Engine
}

func (s *SnapshotStrategy) PrepareMigration(ctx context.Context, job *MigrationJob) error {
	s.engine.logger.Info("preparing snapshot migration",
		zap.String("job_id", job.ID),
	)

	// Snapshot strategy doesn't support pause/resume
	job.CanPause = false
	job.CanResume = false

	// Verify snapshot capability
	// This would check for LVM/ZFS on the host
	// For now, return not implemented

	return fmt.Errorf("snapshot strategy requires LVM or ZFS, not yet implemented")
}

func (s *SnapshotStrategy) ExecuteMigration(ctx context.Context, job *MigrationJob, progressCh chan<- MigrationProgress) error {
	// Would create LVM/ZFS snapshot and transfer
	return fmt.Errorf("not implemented")
}

func (s *SnapshotStrategy) Rollback(ctx context.Context, job *MigrationJob) error {
	// Would remove snapshot
	return nil
}
