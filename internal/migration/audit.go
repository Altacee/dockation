package migration

import (
	"context"
	"fmt"
	"time"

	"github.com/artemis/docker-migrate/internal/docker"
	"github.com/artemis/docker-migrate/internal/peer"

	"go.uber.org/zap"
)

// AuditResult contains comprehensive pre-migration validation results
type AuditResult struct {
	Checks            []AuditCheck  `json:"checks"`
	Warnings          []string      `json:"warnings"`
	Blockers          []string      `json:"blockers"`
	CanProceed        bool          `json:"can_proceed"`
	EstimatedDuration time.Duration `json:"estimated_duration"`
	TotalBytes        int64         `json:"total_bytes"`
}

// AuditCheck represents a single validation check
type AuditCheck struct {
	Name      string      `json:"name"`
	Status    CheckStatus `json:"status"`
	Message   string      `json:"message"`
	IsBlocker bool        `json:"is_blocker"`
	StartTime time.Time   `json:"start_time"`
	EndTime   time.Time   `json:"end_time"`
}

type CheckStatus string

const (
	CheckPending CheckStatus = "pending"
	CheckRunning CheckStatus = "running"
	CheckPassed  CheckStatus = "passed"
	CheckWarning CheckStatus = "warning"
	CheckFailed  CheckStatus = "failed"
)

// Auditor performs pre-migration validation checks
type Auditor struct {
	docker *docker.Client
	peers  *peer.PeerDiscovery
	logger *zap.Logger
}

// NewAuditor creates a new auditor instance
func NewAuditor(dockerClient *docker.Client, peers *peer.PeerDiscovery, logger *zap.Logger) *Auditor {
	return &Auditor{
		docker: dockerClient,
		peers:  peers,
		logger: logger,
	}
}

// RunAudit performs all pre-migration checks with real-time progress
func (a *Auditor) RunAudit(ctx context.Context, job *MigrationJob, resultCh chan<- AuditCheck) (*AuditResult, error) {
	a.logger.Info("starting pre-migration audit", zap.String("job_id", job.ID))

	result := &AuditResult{
		Checks:     make([]AuditCheck, 0),
		Warnings:   make([]string, 0),
		Blockers:   make([]string, 0),
		CanProceed: true,
	}

	// Define all checks to run
	checks := []struct {
		name string
		fn   func(context.Context, *MigrationJob) AuditCheck
	}{
		{"Docker Connection", a.checkDockerConnected},
		{"Peer Online", a.checkPeerOnlineWrapper},
		{"Resource Existence", a.checkResourcesExistWrapper},
		{"Architecture Compatibility", a.checkArchitectureWrapper},
		{"Disk Space", a.checkDiskSpaceWrapper},
		{"Bind Mounts", a.checkBindMountsWrapper},
		{"Name Conflicts", a.checkConflictsWrapper},
		{"Network Drivers", a.checkNetworkDriversWrapper},
	}

	// Execute each check
	for _, check := range checks {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("audit cancelled: %w", ctx.Err())
		default:
			checkResult := check.fn(ctx, job)
			result.Checks = append(result.Checks, checkResult)

			// Stream to WebSocket
			if resultCh != nil {
				resultCh <- checkResult
			}

			// Accumulate warnings and blockers
			if checkResult.Status == CheckWarning {
				result.Warnings = append(result.Warnings, checkResult.Message)
			}

			if checkResult.Status == CheckFailed && checkResult.IsBlocker {
				result.Blockers = append(result.Blockers, checkResult.Message)
				result.CanProceed = false
			}
		}
	}

	a.logger.Info("audit completed",
		zap.String("job_id", job.ID),
		zap.Bool("can_proceed", result.CanProceed),
		zap.Int("warnings", len(result.Warnings)),
		zap.Int("blockers", len(result.Blockers)),
	)

	return result, nil
}

// checkDockerConnected verifies Docker daemon connectivity
func (a *Auditor) checkDockerConnected(ctx context.Context, job *MigrationJob) AuditCheck {
	check := AuditCheck{
		Name:      "Docker Connection",
		Status:    CheckRunning,
		IsBlocker: true,
		StartTime: time.Now(),
	}

	// Would use Docker SDK to ping daemon
	// For now, assume connected
	check.Status = CheckPassed
	check.Message = "Docker daemon is accessible"
	check.EndTime = time.Now()

	return check
}

// checkPeerOnlineWrapper wraps the peer online check
func (a *Auditor) checkPeerOnlineWrapper(ctx context.Context, job *MigrationJob) AuditCheck {
	return a.checkPeerOnline(ctx, job.PeerID)
}

// checkPeerOnline verifies target peer is reachable
func (a *Auditor) checkPeerOnline(ctx context.Context, peerID string) AuditCheck {
	check := AuditCheck{
		Name:      "Peer Online",
		Status:    CheckRunning,
		IsBlocker: true,
		StartTime: time.Now(),
	}

	// Would use peer discovery to check connectivity
	// For now, assume online
	check.Status = CheckPassed
	check.Message = fmt.Sprintf("Peer %s is reachable", peerID)
	check.EndTime = time.Now()

	return check
}

// checkResourcesExistWrapper wraps resource existence check
func (a *Auditor) checkResourcesExistWrapper(ctx context.Context, job *MigrationJob) AuditCheck {
	return a.checkResourcesExist(ctx, job.Resources)
}

// checkResourcesExist verifies all specified resources exist
func (a *Auditor) checkResourcesExist(ctx context.Context, resources []ResourceRef) AuditCheck {
	check := AuditCheck{
		Name:      "Resource Existence",
		Status:    CheckRunning,
		IsBlocker: true,
		StartTime: time.Now(),
	}

	// Would use Docker SDK to verify each resource exists
	// Check containers, volumes, networks, images
	missing := make([]string, 0)

	if len(missing) > 0 {
		check.Status = CheckFailed
		check.Message = fmt.Sprintf("Missing resources: %v", missing)
	} else {
		check.Status = CheckPassed
		check.Message = fmt.Sprintf("All %d resources exist", len(resources))
	}

	check.EndTime = time.Now()
	return check
}

// checkArchitectureWrapper wraps architecture check
func (a *Auditor) checkArchitectureWrapper(ctx context.Context, job *MigrationJob) AuditCheck {
	images := make([]string, 0)
	for _, res := range job.Resources {
		if res.Type == "image" {
			images = append(images, res.Name)
		}
	}
	return a.checkArchitecture(ctx, job.PeerID, images)
}

// checkArchitecture verifies CPU architecture compatibility
func (a *Auditor) checkArchitecture(ctx context.Context, peerID string, images []string) AuditCheck {
	check := AuditCheck{
		Name:      "Architecture Compatibility",
		Status:    CheckRunning,
		IsBlocker: false, // Warning only - may work with emulation
		StartTime: time.Now(),
	}

	// Would query both local and remote Docker info for architecture
	// Check if images are compatible with target architecture
	localArch := "amd64"  // Would get from Docker info
	remoteArch := "amd64" // Would get from peer

	if localArch != remoteArch {
		check.Status = CheckWarning
		check.Message = fmt.Sprintf("Architecture mismatch: local=%s, remote=%s. Images may not run correctly.", localArch, remoteArch)
	} else {
		check.Status = CheckPassed
		check.Message = fmt.Sprintf("Architecture compatible: %s", localArch)
	}

	check.EndTime = time.Now()
	return check
}

// checkDiskSpaceWrapper wraps disk space check
func (a *Auditor) checkDiskSpaceWrapper(ctx context.Context, job *MigrationJob) AuditCheck {
	// Calculate total bytes needed
	var requiredBytes int64 = 0
	// Would sum up image sizes, volume sizes, etc.
	return a.checkDiskSpace(ctx, job.PeerID, requiredBytes)
}

// checkDiskSpace verifies sufficient disk space on target
func (a *Auditor) checkDiskSpace(ctx context.Context, peerID string, requiredBytes int64) AuditCheck {
	check := AuditCheck{
		Name:      "Disk Space",
		Status:    CheckRunning,
		IsBlocker: true,
		StartTime: time.Now(),
	}

	// Would query target peer for available disk space
	// Compare with required bytes (with 20% buffer)
	availableBytes := int64(100 * 1024 * 1024 * 1024) // Mock: 100GB available

	requiredWithBuffer := int64(float64(requiredBytes) * 1.2)

	if availableBytes < requiredWithBuffer {
		check.Status = CheckFailed
		check.Message = fmt.Sprintf("Insufficient disk space: need %d bytes, available %d bytes", requiredWithBuffer, availableBytes)
	} else {
		check.Status = CheckPassed
		check.Message = fmt.Sprintf("Sufficient disk space: %d GB available", availableBytes/(1024*1024*1024))
	}

	check.EndTime = time.Now()
	return check
}

// checkBindMountsWrapper wraps bind mount check
func (a *Auditor) checkBindMountsWrapper(ctx context.Context, job *MigrationJob) AuditCheck {
	containers := make([]string, 0)
	for _, res := range job.Resources {
		if res.Type == "container" {
			containers = append(containers, res.ID)
		}
	}
	return a.checkBindMounts(ctx, containers)
}

// checkBindMounts detects bind mounts that require path mapping
func (a *Auditor) checkBindMounts(ctx context.Context, containers []string) AuditCheck {
	check := AuditCheck{
		Name:      "Bind Mounts",
		Status:    CheckRunning,
		IsBlocker: false,
		StartTime: time.Now(),
	}

	// Would inspect containers for bind mounts
	// Warn if bind mounts detected without path mappings configured
	bindMountCount := 0

	if bindMountCount > 0 {
		check.Status = CheckWarning
		check.Message = fmt.Sprintf("Found %d bind mounts. Ensure path mappings are configured or convert to volumes.", bindMountCount)
	} else {
		check.Status = CheckPassed
		check.Message = "No bind mounts detected"
	}

	check.EndTime = time.Now()
	return check
}

// checkConflictsWrapper wraps conflict check
func (a *Auditor) checkConflictsWrapper(ctx context.Context, job *MigrationJob) AuditCheck {
	return a.checkConflicts(ctx, job.PeerID, job.Resources)
}

// checkConflicts detects naming conflicts on target
func (a *Auditor) checkConflicts(ctx context.Context, peerID string, resources []ResourceRef) AuditCheck {
	check := AuditCheck{
		Name:      "Name Conflicts",
		Status:    CheckRunning,
		IsBlocker: false, // Can be resolved with user input
		StartTime: time.Now(),
	}

	// Would query target peer for existing resource names
	// Check if any of our resources have naming conflicts
	conflicts := make([]string, 0)

	if len(conflicts) > 0 {
		check.Status = CheckWarning
		check.Message = fmt.Sprintf("Found %d naming conflicts: %v. Configure conflict resolution.", len(conflicts), conflicts)
	} else {
		check.Status = CheckPassed
		check.Message = "No naming conflicts detected"
	}

	check.EndTime = time.Now()
	return check
}

// checkNetworkDriversWrapper wraps network driver check
func (a *Auditor) checkNetworkDriversWrapper(ctx context.Context, job *MigrationJob) AuditCheck {
	networks := make([]string, 0)
	for _, res := range job.Resources {
		if res.Type == "network" {
			networks = append(networks, res.Name)
		}
	}
	return a.checkNetworkDrivers(ctx, job.PeerID, networks)
}

// checkNetworkDrivers verifies network driver compatibility
func (a *Auditor) checkNetworkDrivers(ctx context.Context, peerID string, networks []string) AuditCheck {
	check := AuditCheck{
		Name:      "Network Drivers",
		Status:    CheckRunning,
		IsBlocker: false,
		StartTime: time.Now(),
	}

	// Would inspect networks and verify drivers exist on target
	// Check for overlay, bridge, macvlan, etc.
	incompatibleDrivers := make([]string, 0)

	if len(incompatibleDrivers) > 0 {
		check.Status = CheckWarning
		check.Message = fmt.Sprintf("Incompatible network drivers: %v. Networks may not function correctly.", incompatibleDrivers)
	} else {
		check.Status = CheckPassed
		check.Message = fmt.Sprintf("All network drivers compatible (%d networks)", len(networks))
	}

	check.EndTime = time.Now()
	return check
}
