package migration

import (
	"context"
	"fmt"
	"time"

	"github.com/artemis/docker-migrate/internal/docker"
	"github.com/artemis/docker-migrate/internal/peer"

	"go.uber.org/zap"
)

// ConflictResolver handles resource naming conflicts on target
type ConflictResolver struct {
	docker *docker.Client
	peers  *peer.PeerDiscovery
	logger *zap.Logger
}

// Conflict represents a naming conflict
type Conflict struct {
	Type       ConflictType `json:"type"`
	LocalName  string       `json:"local_name"`
	RemoteName string       `json:"remote_name"`
	Details    string       `json:"details"`
}

type ConflictType string

const (
	ConflictContainer ConflictType = "container"
	ConflictVolume    ConflictType = "volume"
	ConflictNetwork   ConflictType = "network"
	ConflictImage     ConflictType = "image"
)

// Resolution defines how to handle a conflict
type Resolution string

const (
	ResolutionOverwrite Resolution = "overwrite" // Remove existing, use incoming
	ResolutionRename    Resolution = "rename"    // Rename incoming resource
	ResolutionSkip      Resolution = "skip"      // Skip this resource
	ResolutionAbort     Resolution = "abort"     // Stop migration
)

// NewConflictResolver creates a conflict resolver
func NewConflictResolver(dockerClient *docker.Client, peers *peer.PeerDiscovery, logger *zap.Logger) *ConflictResolver {
	return &ConflictResolver{
		docker: dockerClient,
		peers:  peers,
		logger: logger,
	}
}

// DetectConflicts checks for naming conflicts on target
func (cr *ConflictResolver) DetectConflicts(ctx context.Context, peerID string, resources []ResourceRef) ([]Conflict, error) {
	cr.logger.Info("detecting conflicts",
		zap.String("peer_id", peerID),
		zap.Int("resource_count", len(resources)),
	)

	conflicts := make([]Conflict, 0)

	// Would query target peer for existing resources
	// Check each resource name against target
	for _, resource := range resources {
		// Mock: assume no conflicts for now
		// In production, would send gRPC request to target
		// to check if resource with same name exists

		exists := false // Would be result of gRPC call

		if exists {
			conflict := Conflict{
				Type:       ConflictType(resource.Type),
				LocalName:  resource.Name,
				RemoteName: resource.Name,
				Details:    fmt.Sprintf("%s '%s' already exists on target", resource.Type, resource.Name),
			}
			conflicts = append(conflicts, conflict)
		}
	}

	cr.logger.Info("conflict detection complete",
		zap.Int("conflicts_found", len(conflicts)),
	)

	return conflicts, nil
}

// ApplyResolution executes the chosen resolution
func (cr *ConflictResolver) ApplyResolution(ctx context.Context, conflict Conflict, resolution Resolution) error {
	cr.logger.Info("applying conflict resolution",
		zap.String("conflict_type", string(conflict.Type)),
		zap.String("resource", conflict.LocalName),
		zap.String("resolution", string(resolution)),
	)

	switch resolution {
	case ResolutionOverwrite:
		return cr.overwriteResource(ctx, conflict)

	case ResolutionRename:
		return cr.renameResource(ctx, conflict)

	case ResolutionSkip:
		cr.logger.Info("skipping conflicting resource",
			zap.String("resource", conflict.LocalName),
		)
		return nil

	case ResolutionAbort:
		return fmt.Errorf("migration aborted due to conflict: %s", conflict.Details)

	default:
		return fmt.Errorf("unknown resolution: %s", resolution)
	}
}

// overwriteResource removes existing resource and replaces with incoming
func (cr *ConflictResolver) overwriteResource(ctx context.Context, conflict Conflict) error {
	cr.logger.Warn("overwriting existing resource",
		zap.String("type", string(conflict.Type)),
		zap.String("name", conflict.RemoteName),
	)

	// Would send gRPC request to target to:
	// 1. Stop/remove existing resource
	// 2. Allow migration to proceed with same name

	return nil
}

// renameResource modifies incoming resource name to avoid conflict
func (cr *ConflictResolver) renameResource(ctx context.Context, conflict Conflict) error {
	// Generate unique name by appending timestamp
	timestamp := time.Now().Format("20060102-150405")
	newName := fmt.Sprintf("%s-migrated-%s", conflict.LocalName, timestamp)

	cr.logger.Info("renaming resource to avoid conflict",
		zap.String("original_name", conflict.LocalName),
		zap.String("new_name", newName),
	)

	// Would update resource name in migration job
	// This needs to be propagated back to the job's resource list

	return nil
}

// GenerateUniqueName creates a unique name for a resource
func (cr *ConflictResolver) GenerateUniqueName(baseName string, conflictType ConflictType) string {
	timestamp := time.Now().Format("20060102-150405")
	return fmt.Sprintf("%s-migrated-%s", baseName, timestamp)
}
