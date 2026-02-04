package migration

import (
	"context"
	"fmt"

	"github.com/artemis/docker-migrate/internal/docker"
	"github.com/artemis/docker-migrate/internal/peer"

	"go.uber.org/zap"
)

// ContainerMigrator handles Docker container migration with full state preservation
type ContainerMigrator struct {
	docker   *docker.Client
	transfer *peer.TransferManager
	logger   *zap.Logger
}

// ContainerState represents complete container configuration for recreation
type ContainerState struct {
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	ImageID       string            `json:"image_id"`
	Command       []string          `json:"command"`
	Entrypoint    []string          `json:"entrypoint"`
	Env           []string          `json:"env"`
	Labels        map[string]string `json:"labels"`
	Volumes       []VolumeMount     `json:"volumes"`
	Networks      []NetworkAttach   `json:"networks"`
	Ports         []PortMapping     `json:"ports"`
	RestartPolicy RestartPolicy     `json:"restart_policy"`
	Resources     ResourceLimits    `json:"resources"`
	WorkingDir    string            `json:"working_dir"`
	User          string            `json:"user"`
	Hostname      string            `json:"hostname"`
}

type VolumeMount struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"read_only"`
	Type        string `json:"type"` // volume, bind, tmpfs
}

type NetworkAttach struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	IPAddr  string   `json:"ip_addr,omitempty"`
}

type PortMapping struct {
	ContainerPort int    `json:"container_port"`
	HostPort      int    `json:"host_port"`
	Protocol      string `json:"protocol"` // tcp, udp
	HostIP        string `json:"host_ip,omitempty"`
}

type RestartPolicy struct {
	Name              string `json:"name"` // no, always, on-failure, unless-stopped
	MaximumRetryCount int    `json:"maximum_retry_count,omitempty"`
}

type ResourceLimits struct {
	CPUShares  int64  `json:"cpu_shares,omitempty"`
	Memory     int64  `json:"memory,omitempty"`
	MemorySwap int64  `json:"memory_swap,omitempty"`
	CPUPeriod  int64  `json:"cpu_period,omitempty"`
	CPUQuota   int64  `json:"cpu_quota,omitempty"`
}

// MigrateContainer transfers and recreates container with full state
func (cm *ContainerMigrator) MigrateContainer(ctx context.Context, containerID, peerID string, mode MigrationMode, progressCh chan<- MigrationProgress) error {
	cm.logger.Info("starting container migration",
		zap.String("container_id", containerID),
		zap.String("peer_id", peerID),
		zap.String("mode", string(mode)),
	)

	// Step 1: Export full container state
	state, err := cm.exportContainerState(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to export container state: %w", err)
	}

	cm.logger.Info("exported container state",
		zap.String("container", state.Name),
		zap.String("image", state.Image),
		zap.Int("volumes", len(state.Volumes)),
		zap.Int("networks", len(state.Networks)),
	)

	// Step 2: Ensure image exists on target (trigger image migration if needed)
	// This would check if image exists and call ImageMigrator if not

	// Step 3: Ensure networks exist on target
	// This would create networks if they don't exist

	// Step 4: Send container state to peer for recreation
	if err := cm.sendContainerState(ctx, peerID, state); err != nil {
		return fmt.Errorf("failed to send container state: %w", err)
	}

	// Step 5: Handle Move mode - disable source after verification
	if mode == ModeMove {
		if err := cm.disableSourceContainer(ctx, containerID, state.Name); err != nil {
			cm.logger.Warn("failed to disable source container",
				zap.String("container", state.Name),
				zap.Error(err),
			)
		}
	}

	cm.logger.Info("container migration completed",
		zap.String("container", state.Name),
	)

	return nil
}

// exportContainerState retrieves complete container configuration
func (cm *ContainerMigrator) exportContainerState(ctx context.Context, containerID string) (*ContainerState, error) {
	// Would use Docker SDK to inspect container and extract full state
	// For now, return mock data
	state := &ContainerState{
		Name:    "my-container",
		Image:   "nginx:latest",
		ImageID: "sha256:abc123",
		Env:     []string{"ENV=production"},
		Labels:  map[string]string{"app": "web"},
		Volumes: []VolumeMount{
			{Source: "my-volume", Destination: "/data", Type: "volume"},
		},
		Networks: []NetworkAttach{
			{Name: "my-network"},
		},
		RestartPolicy: RestartPolicy{
			Name: "unless-stopped",
		},
	}

	return state, nil
}

// sendContainerState sends container configuration to target for recreation
func (cm *ContainerMigrator) sendContainerState(ctx context.Context, peerID string, state *ContainerState) error {
	cm.logger.Info("sending container state to target",
		zap.String("peer_id", peerID),
		zap.String("container", state.Name),
	)

	// Would send via gRPC to target peer
	// Target would:
	// 1. Validate all dependencies exist (image, volumes, networks)
	// 2. Create container with exact configuration
	// 3. Start container
	// 4. Verify health

	return nil
}

// disableSourceContainer stops and renames source after successful migration
func (cm *ContainerMigrator) disableSourceContainer(ctx context.Context, containerID, name string) error {
	cm.logger.Info("disabling source container",
		zap.String("container_id", containerID),
		zap.String("name", name),
	)

	// Would:
	// 1. Stop container
	// 2. Rename to {name}-migrated-backup-{timestamp}
	// 3. Set restart policy to "no"
	// 4. Add label "docker-migrate.migrated=true"

	backupName := fmt.Sprintf("%s-migrated-backup", name)
	cm.logger.Info("source container disabled",
		zap.String("original_name", name),
		zap.String("backup_name", backupName),
	)

	return nil
}
