package worker

import (
	"context"

	"github.com/artemis/docker-migrate/internal/docker"
	"github.com/artemis/docker-migrate/internal/observability"
	pb "github.com/artemis/docker-migrate/proto"
	"go.uber.org/zap"
)

// Inventory scans Docker resources
type Inventory struct {
	docker *docker.Client
	logger *observability.Logger
}

// NewInventory creates a new inventory scanner
func NewInventory(dockerClient *docker.Client, logger *observability.Logger) *Inventory {
	return &Inventory{
		docker: dockerClient,
		logger: logger,
	}
}

// Scan scans all Docker resources
func (i *Inventory) Scan(ctx context.Context) (*pb.ResourceInventory, error) {
	inv := &pb.ResourceInventory{
		Containers: make([]*pb.ContainerResource, 0),
		Images:     make([]*pb.ImageResource, 0),
		Volumes:    make([]*pb.VolumeResource, 0),
		Networks:   make([]*pb.NetworkResource, 0),
	}

	// Scan containers
	containers, err := i.docker.ListContainers(ctx, true)
	if err != nil {
		i.logger.Error("failed to list containers", zap.Error(err))
	} else {
		for _, c := range containers {
			name := ""
			if len(c.Names) > 0 {
				name = c.Names[0]
			}
			inv.Containers = append(inv.Containers, &pb.ContainerResource{
				Id:      c.ID,
				Name:    name,
				Image:   c.Image,
				State:   c.State,
				Created: c.Created,
				Labels:  c.Labels,
			})
		}
	}

	// Scan images
	images, err := i.docker.ListImages(ctx)
	if err != nil {
		i.logger.Error("failed to list images", zap.Error(err))
	} else {
		for _, img := range images {
			inv.Images = append(inv.Images, &pb.ImageResource{
				Id:         img.ID,
				Tags:       img.RepoTags,
				Size:       img.Size,
				Created:    img.Created,
				LayerCount: int32(len(img.RepoDigests)),
			})
		}
	}

	// Scan volumes
	volumes, err := i.docker.ListVolumes(ctx)
	if err != nil {
		i.logger.Error("failed to list volumes", zap.Error(err))
	} else {
		for _, vol := range volumes {
			inv.Volumes = append(inv.Volumes, &pb.VolumeResource{
				Name:       vol.Name,
				Driver:     vol.Driver,
				Mountpoint: vol.Mountpoint,
				Labels:     vol.Labels,
			})
		}
	}

	// Scan networks
	networks, err := i.docker.ListNetworks(ctx)
	if err != nil {
		i.logger.Error("failed to list networks", zap.Error(err))
	} else {
		for _, net := range networks {
			inv.Networks = append(inv.Networks, &pb.NetworkResource{
				Id:             net.ID,
				Name:           net.Name,
				Driver:         net.Driver,
				Scope:          net.Scope,
				Internal:       net.Internal,
				ContainerCount: int32(len(net.Containers)),
			})
		}
	}

	i.logger.Debug("inventory scan complete",
		zap.Int("containers", len(inv.Containers)),
		zap.Int("images", len(inv.Images)),
		zap.Int("volumes", len(inv.Volumes)),
		zap.Int("networks", len(inv.Networks)),
	)

	return inv, nil
}
