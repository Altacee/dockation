package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"go.uber.org/zap"
)

// NetworkInfo represents detailed network information
type NetworkInfo struct {
	ID         string                      `json:"id"`
	Name       string                      `json:"name"`
	Driver     string                      `json:"driver"`
	Scope      string                      `json:"scope"`
	Internal   bool                        `json:"internal"`
	Attachable bool                        `json:"attachable"`
	Ingress    bool                        `json:"ingress"`
	IPAM       network.IPAM                `json:"ipam"`
	Options    map[string]string           `json:"options"`
	Labels     map[string]string           `json:"labels"`
	Containers map[string]types.EndpointResource `json:"containers"`
}

// ListNetworks returns all networks
func (c *Client) ListNetworks(ctx context.Context) ([]types.NetworkResource, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	networks, err := cli.NetworkList(ctx, types.NetworkListOptions{})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("network_list").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("network_list", "error").Inc()
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	observability.DockerOperations.WithLabelValues("network_list", "success").Inc()
	c.logger.Info("listed networks", zap.Int("count", len(networks)))

	return networks, nil
}

// InspectNetwork retrieves detailed network information
func (c *Client) InspectNetwork(ctx context.Context, networkID string) (types.NetworkResource, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return types.NetworkResource{}, fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	net, err := cli.NetworkInspect(ctx, networkID, types.NetworkInspectOptions{})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("network_inspect").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("network_inspect", "error").Inc()
		return types.NetworkResource{}, fmt.Errorf("failed to inspect network %s: %w", networkID, err)
	}

	observability.DockerOperations.WithLabelValues("network_inspect", "success").Inc()
	return net, nil
}

// ExportNetwork exports complete network configuration
func (c *Client) ExportNetwork(ctx context.Context, networkID string) (*NetworkInfo, error) {
	c.logger.Info("exporting network", zap.String("network_id", networkID))

	net, err := c.InspectNetwork(ctx, networkID)
	if err != nil {
		return nil, err
	}

	info := &NetworkInfo{
		ID:         net.ID,
		Name:       net.Name,
		Driver:     net.Driver,
		Scope:      net.Scope,
		Internal:   net.Internal,
		Attachable: net.Attachable,
		Ingress:    net.Ingress,
		IPAM:       net.IPAM,
		Options:    net.Options,
		Labels:     net.Labels,
		Containers: net.Containers,
	}

	c.logger.Info("network exported",
		zap.String("network_id", networkID),
		zap.String("name", info.Name),
		zap.String("driver", info.Driver),
		zap.Int("containers", len(info.Containers)),
	)

	return info, nil
}

// CreateNetwork creates a network from exported configuration
func (c *Client) CreateNetwork(ctx context.Context, info *NetworkInfo, newName string) (string, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return "", fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	name := newName
	if name == "" {
		name = info.Name
	}

	c.logger.Info("creating network",
		zap.String("name", name),
		zap.String("driver", info.Driver),
	)

	// Skip built-in networks
	if isBuiltInNetwork(name) {
		c.logger.Info("skipping built-in network", zap.String("name", name))
		return "", fmt.Errorf("cannot recreate built-in network: %s", name)
	}

	start := time.Now()
	resp, err := cli.NetworkCreate(ctx, name, types.NetworkCreate{
		CheckDuplicate: true,
		Driver:         info.Driver,
		Scope:          info.Scope,
		EnableIPv6:     false,
		IPAM:           &info.IPAM,
		Internal:       info.Internal,
		Attachable:     info.Attachable,
		Ingress:        info.Ingress,
		Options:        info.Options,
		Labels:         info.Labels,
	})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("network_create").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("network_create", "error").Inc()
		return "", fmt.Errorf("failed to create network %s: %w", name, err)
	}

	observability.DockerOperations.WithLabelValues("network_create", "success").Inc()

	if resp.Warning != "" {
		c.logger.Warn("network creation warning",
			zap.String("network_id", resp.ID),
			zap.String("warning", resp.Warning),
		)
	}

	c.logger.Info("network created",
		zap.String("network_id", resp.ID),
		zap.String("name", name),
	)

	return resp.ID, nil
}

// RemoveNetwork removes a network
func (c *Client) RemoveNetwork(ctx context.Context, networkID string) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	// Get network name for validation
	net, err := c.InspectNetwork(ctx, networkID)
	if err != nil {
		return err
	}

	// Prevent removal of built-in networks
	if isBuiltInNetwork(net.Name) {
		return fmt.Errorf("cannot remove built-in network: %s", net.Name)
	}

	start := time.Now()
	err = cli.NetworkRemove(ctx, networkID)
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("network_remove").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("network_remove", "error").Inc()
		return fmt.Errorf("failed to remove network %s: %w", networkID, err)
	}

	observability.DockerOperations.WithLabelValues("network_remove", "success").Inc()
	c.logger.Info("network removed",
		zap.String("network_id", networkID),
		zap.String("name", net.Name),
	)

	return nil
}

// ConnectContainer connects a container to a network
func (c *Client) ConnectContainer(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	err := cli.NetworkConnect(ctx, networkID, containerID, config)
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("network_connect").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("network_connect", "error").Inc()
		return fmt.Errorf("failed to connect container to network: %w", err)
	}

	observability.DockerOperations.WithLabelValues("network_connect", "success").Inc()
	c.logger.Info("container connected to network",
		zap.String("network_id", networkID),
		zap.String("container_id", containerID),
	)

	return nil
}

// DisconnectContainer disconnects a container from a network
func (c *Client) DisconnectContainer(ctx context.Context, networkID, containerID string, force bool) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	err := cli.NetworkDisconnect(ctx, networkID, containerID, force)
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("network_disconnect").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("network_disconnect", "error").Inc()
		return fmt.Errorf("failed to disconnect container from network: %w", err)
	}

	observability.DockerOperations.WithLabelValues("network_disconnect", "success").Inc()
	c.logger.Info("container disconnected from network",
		zap.String("network_id", networkID),
		zap.String("container_id", containerID),
	)

	return nil
}

// isBuiltInNetwork checks if a network is a Docker built-in network
func isBuiltInNetwork(name string) bool {
	builtInNetworks := []string{"bridge", "host", "none"}
	for _, n := range builtInNetworks {
		if name == n {
			return true
		}
	}
	return false
}
