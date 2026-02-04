package docker

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"go.uber.org/zap"
)

// ContainerState represents the complete state of a container
// This preserves all configuration needed for exact recreation
type ContainerState struct {
	ID              string                      `json:"id"`
	Name            string                      `json:"name"`
	Config          *container.Config           `json:"config"`
	HostConfig      *container.HostConfig       `json:"host_config"`
	NetworkSettings *network.NetworkingConfig   `json:"network_settings"`
	Mounts          []mount.Mount               `json:"mounts"`
	Created         time.Time                   `json:"created"`
	State           *types.ContainerState       `json:"state"`
	Image           string                      `json:"image"`
	ImageID         string                      `json:"image_id"`
}

// ListContainers returns all containers with full inspect data
func (c *Client) ListContainers(ctx context.Context, all bool) ([]types.Container, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: all})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("container_list").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("container_list", "error").Inc()
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	observability.DockerOperations.WithLabelValues("container_list", "success").Inc()
	c.logger.Info("listed containers",
		zap.Int("count", len(containers)),
		zap.Bool("all", all),
	)

	return containers, nil
}

// InspectContainer retrieves detailed container information
func (c *Client) InspectContainer(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return types.ContainerJSON{}, fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	inspect, err := cli.ContainerInspect(ctx, containerID)
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("container_inspect").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("container_inspect", "error").Inc()
		return types.ContainerJSON{}, fmt.Errorf("failed to inspect container %s: %w", containerID, err)
	}

	observability.DockerOperations.WithLabelValues("container_inspect", "success").Inc()
	return inspect, nil
}

// ExportContainerState exports the complete state of a container
// This captures everything needed to recreate the container identically
func (c *Client) ExportContainerState(ctx context.Context, containerID string) (*ContainerState, error) {
	c.logger.Info("exporting container state", zap.String("container_id", containerID))

	inspect, err := c.InspectContainer(ctx, containerID)
	if err != nil {
		return nil, err
	}

	// Build network configuration from current settings
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: make(map[string]*network.EndpointSettings),
	}

	// Preserve all network connections
	if inspect.NetworkSettings != nil {
		for netName, netSettings := range inspect.NetworkSettings.Networks {
			networkConfig.EndpointsConfig[netName] = &network.EndpointSettings{
				IPAMConfig: netSettings.IPAMConfig,
				Links:      netSettings.Links,
				Aliases:    netSettings.Aliases,
				NetworkID:  netSettings.NetworkID,
				MacAddress: netSettings.MacAddress,
			}
		}
	}

	// Convert binds to mounts for more reliable recreation
	mounts := make([]mount.Mount, 0, len(inspect.Mounts))
	for _, m := range inspect.Mounts {
		mounts = append(mounts, mount.Mount{
			Type:     m.Type,
			Source:   m.Source,
			Target:   m.Destination,
			ReadOnly: !m.RW,
		})
	}

	// Sanitize environment variables for logging
	redactedEnv := observability.RedactEnv(inspect.Config.Env)
	c.logger.InfoRedacted("container environment variables sanitized",
		zap.Int("total_vars", len(inspect.Config.Env)),
		zap.Strings("redacted", redactedEnv[:min(5, len(redactedEnv))]),
	)

	// Parse created time
	createdTime, _ := time.Parse(time.RFC3339Nano, inspect.Created)

	state := &ContainerState{
		ID:              inspect.ID,
		Name:            inspect.Name,
		Config:          inspect.Config,
		HostConfig:      inspect.HostConfig,
		NetworkSettings: networkConfig,
		Mounts:          mounts,
		Created:         createdTime,
		State:           inspect.State,
		Image:           inspect.Config.Image,
		ImageID:         inspect.Image,
	}

	// Validate state completeness
	if err := validateContainerState(state); err != nil {
		return nil, fmt.Errorf("container state validation failed: %w", err)
	}

	c.logger.Info("container state exported successfully",
		zap.String("container_id", containerID),
		zap.String("name", state.Name),
		zap.String("image", state.Image),
		zap.Int("mounts", len(state.Mounts)),
		zap.Int("networks", len(networkConfig.EndpointsConfig)),
	)

	return state, nil
}

// CreateContainer creates a container from exported state
func (c *Client) CreateContainer(ctx context.Context, state *ContainerState, newName string) (string, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return "", fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	// Validate state before attempting creation
	if err := validateContainerState(state); err != nil {
		return "", fmt.Errorf("invalid container state: %w", err)
	}

	// Use provided name or original name
	name := newName
	if name == "" {
		name = state.Name
	}

	c.logger.Info("creating container from state",
		zap.String("name", name),
		zap.String("image", state.Image),
	)

	// Clear runtime-specific fields that shouldn't be set on creation
	config := *state.Config
	hostConfig := *state.HostConfig

	// Apply mounts to host config
	hostConfig.Mounts = state.Mounts

	start := time.Now()
	resp, err := cli.ContainerCreate(
		ctx,
		&config,
		&hostConfig,
		state.NetworkSettings,
		nil, // Platform
		name,
	)
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("container_create").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("container_create", "error").Inc()
		c.logger.Error("failed to create container",
			zap.String("name", name),
			zap.Error(err),
		)
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	observability.DockerOperations.WithLabelValues("container_create", "success").Inc()

	// Log any warnings from Docker
	for _, warning := range resp.Warnings {
		c.logger.Warn("container creation warning",
			zap.String("container_id", resp.ID),
			zap.String("warning", warning),
		)
	}

	c.logger.Info("container created successfully",
		zap.String("container_id", resp.ID),
		zap.String("name", name),
	)

	// Verify container was created with correct configuration
	if err := c.verifyContainerCreation(ctx, resp.ID, state); err != nil {
		// Attempt cleanup on verification failure
		_ = c.RemoveContainer(ctx, resp.ID, true)
		return "", fmt.Errorf("container verification failed: %w", err)
	}

	return resp.ID, nil
}

// RemoveContainer removes a container with force option
func (c *Client) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	err := cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: force})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("container_remove").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("container_remove", "error").Inc()
		return fmt.Errorf("failed to remove container %s: %w", containerID, err)
	}

	observability.DockerOperations.WithLabelValues("container_remove", "success").Inc()
	c.logger.Info("container removed", zap.String("container_id", containerID))
	return nil
}

// validateContainerState performs comprehensive validation of container state
func validateContainerState(state *ContainerState) error {
	if state == nil {
		return fmt.Errorf("state is nil")
	}

	if state.Config == nil {
		return fmt.Errorf("config is nil")
	}

	if state.HostConfig == nil {
		return fmt.Errorf("host_config is nil")
	}

	if state.Image == "" {
		return fmt.Errorf("image is empty")
	}

	// Validate critical configuration fields
	if state.Config.Image == "" {
		return fmt.Errorf("config.image is empty")
	}

	return nil
}

// verifyContainerCreation verifies the created container matches expected state
func (c *Client) verifyContainerCreation(ctx context.Context, containerID string, expectedState *ContainerState) error {
	inspect, err := c.InspectContainer(ctx, containerID)
	if err != nil {
		return err
	}

	// Verify critical fields match
	if inspect.Config.Image != expectedState.Config.Image {
		return fmt.Errorf("image mismatch: got %s, expected %s", inspect.Config.Image, expectedState.Config.Image)
	}

	if len(inspect.Mounts) != len(expectedState.Mounts) {
		return fmt.Errorf("mount count mismatch: got %d, expected %d", len(inspect.Mounts), len(expectedState.Mounts))
	}

	c.logger.Info("container creation verified",
		zap.String("container_id", containerID),
	)

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// StartContainer starts a stopped container
func (c *Client) StartContainer(ctx context.Context, containerID string) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	err := cli.ContainerStart(ctx, containerID, container.StartOptions{})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("container_start").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("container_start", "error").Inc()
		return fmt.Errorf("failed to start container %s: %w", containerID, err)
	}

	observability.DockerOperations.WithLabelValues("container_start", "success").Inc()
	c.logger.Info("container started", zap.String("container_id", containerID))
	return nil
}

// StopContainer stops a running container
func (c *Client) StopContainer(ctx context.Context, containerID string, timeout *int) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	err := cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: timeout})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("container_stop").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("container_stop", "error").Inc()
		return fmt.Errorf("failed to stop container %s: %w", containerID, err)
	}

	observability.DockerOperations.WithLabelValues("container_stop", "success").Inc()
	c.logger.Info("container stopped", zap.String("container_id", containerID))
	return nil
}

// RestartContainer restarts a container
func (c *Client) RestartContainer(ctx context.Context, containerID string, timeout *int) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	err := cli.ContainerRestart(ctx, containerID, container.StopOptions{Timeout: timeout})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("container_restart").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("container_restart", "error").Inc()
		return fmt.Errorf("failed to restart container %s: %w", containerID, err)
	}

	observability.DockerOperations.WithLabelValues("container_restart", "success").Inc()
	c.logger.Info("container restarted", zap.String("container_id", containerID))
	return nil
}

// GetContainerLogs returns container logs as a reader
func (c *Client) GetContainerLogs(ctx context.Context, containerID string, tail string, follow bool) (io.ReadCloser, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: true,
		Tail:       tail,
	}

	reader, err := cli.ContainerLogs(ctx, containerID, options)
	if err != nil {
		observability.DockerOperations.WithLabelValues("container_logs", "error").Inc()
		return nil, fmt.Errorf("failed to get container logs %s: %w", containerID, err)
	}

	observability.DockerOperations.WithLabelValues("container_logs", "success").Inc()
	c.logger.Info("container logs stream opened", zap.String("container_id", containerID), zap.Bool("follow", follow))
	return reader, nil
}
