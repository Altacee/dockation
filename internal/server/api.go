package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/artemis/docker-migrate/internal/docker"
	"github.com/artemis/docker-migrate/internal/migration"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ListContainers returns all containers
func (s *Server) ListContainers(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	all := c.Query("all") == "true"

	containers, err := s.docker.ListContainers(ctx, all)
	if err != nil {
		s.logger.Error("failed to list containers", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, containers)
}

// GetContainer returns detailed container information
func (s *Server) GetContainer(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	containerID := c.Param("id")

	state, err := s.docker.ExportContainerState(ctx, containerID)
	if err != nil {
		s.logger.Error("failed to get container", zap.String("id", containerID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, state)
}

// StartContainer starts a stopped container
func (s *Server) StartContainer(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	containerID := c.Param("id")

	if err := s.docker.StartContainer(ctx, containerID); err != nil {
		s.logger.Error("failed to start container", zap.String("id", containerID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Broadcast update to all connected clients
	s.hub.Broadcast([]byte(`{"type":"resource_update","resource":"containers"}`))

	c.JSON(http.StatusOK, gin.H{"status": "started", "container_id": containerID})
}

// StopContainer stops a running container
func (s *Server) StopContainer(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	containerID := c.Param("id")

	var timeout *int
	if t := c.Query("timeout"); t != "" {
		if val, err := fmt.Sscanf(t, "%d", new(int)); err == nil && val > 0 {
			timeout = new(int)
			fmt.Sscanf(t, "%d", timeout)
		}
	}

	if err := s.docker.StopContainer(ctx, containerID, timeout); err != nil {
		s.logger.Error("failed to stop container", zap.String("id", containerID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	s.hub.Broadcast([]byte(`{"type":"resource_update","resource":"containers"}`))

	c.JSON(http.StatusOK, gin.H{"status": "stopped", "container_id": containerID})
}

// RestartContainer restarts a container
func (s *Server) RestartContainer(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	containerID := c.Param("id")

	var timeout *int
	if t := c.Query("timeout"); t != "" {
		if val, err := fmt.Sscanf(t, "%d", new(int)); err == nil && val > 0 {
			timeout = new(int)
			fmt.Sscanf(t, "%d", timeout)
		}
	}

	if err := s.docker.RestartContainer(ctx, containerID, timeout); err != nil {
		s.logger.Error("failed to restart container", zap.String("id", containerID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	s.hub.Broadcast([]byte(`{"type":"resource_update","resource":"containers"}`))

	c.JSON(http.StatusOK, gin.H{"status": "restarted", "container_id": containerID})
}

// RemoveContainer removes a container
func (s *Server) RemoveContainer(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	containerID := c.Param("id")
	force := c.Query("force") == "true"

	if err := s.docker.RemoveContainer(ctx, containerID, force); err != nil {
		s.logger.Error("failed to remove container", zap.String("id", containerID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	s.hub.Broadcast([]byte(`{"type":"resource_update","resource":"containers"}`))

	c.JSON(http.StatusOK, gin.H{"status": "removed", "container_id": containerID})
}

// GetContainerLogs streams container logs
func (s *Server) GetContainerLogs(c *gin.Context) {
	containerID := c.Param("id")
	tail := c.DefaultQuery("tail", "100")
	follow := c.Query("follow") == "true"

	ctx := c.Request.Context()
	if !follow {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	reader, err := s.docker.GetContainerLogs(ctx, containerID, tail, follow)
	if err != nil {
		s.logger.Error("failed to get container logs", zap.String("id", containerID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer reader.Close()

	// Set headers for streaming
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("X-Content-Type-Options", "nosniff")

	if follow {
		c.Header("Transfer-Encoding", "chunked")
	}

	// Stream the logs
	c.Stream(func(w io.Writer) bool {
		buf := make([]byte, 8192)
		n, err := reader.Read(buf)
		if n > 0 {
			// Docker log stream has 8-byte header per line, skip it for cleaner output
			output := buf[:n]
			if len(output) > 8 {
				// Parse multiplexed stream format
				w.Write(stripDockerLogHeader(output))
			} else {
				w.Write(output)
			}
		}
		return err == nil
	})
}

// ListImages returns all images
func (s *Server) ListImages(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	images, err := s.docker.ListImages(ctx)
	if err != nil {
		s.logger.Error("failed to list images", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, images)
}

// GetImage returns detailed image information
func (s *Server) GetImage(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	imageID := c.Param("id")

	info, err := s.docker.GetImageInfo(ctx, imageID)
	if err != nil {
		s.logger.Error("failed to get image", zap.String("id", imageID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, info)
}

// PullImage pulls an image from a registry
func (s *Server) PullImage(c *gin.Context) {
	var req struct {
		Image string `json:"image" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Use a longer timeout for image pulls
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()

	if err := s.docker.PullImage(ctx, req.Image); err != nil {
		s.logger.Error("failed to pull image", zap.String("image", req.Image), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	s.hub.Broadcast([]byte(`{"type":"resource_update","resource":"images"}`))

	c.JSON(http.StatusOK, gin.H{"status": "pulled", "image": req.Image})
}

// RemoveImage removes an image
func (s *Server) RemoveImage(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	imageID := c.Param("id")
	force := c.Query("force") == "true"

	if err := s.docker.RemoveImage(ctx, imageID, force); err != nil {
		s.logger.Error("failed to remove image", zap.String("id", imageID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	s.hub.Broadcast([]byte(`{"type":"resource_update","resource":"images"}`))

	c.JSON(http.StatusOK, gin.H{"status": "removed", "image_id": imageID})
}

// ListVolumes returns all volumes
func (s *Server) ListVolumes(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	volumes, err := s.docker.ListVolumes(ctx)
	if err != nil {
		s.logger.Error("failed to list volumes", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Optionally calculate sizes
	includeSize := c.Query("size") == "true"
	var volumeInfos []*VolumeInfo

	if includeSize {
		for _, vol := range volumes {
			info, err := s.docker.GetVolumeInfo(ctx, vol.Name)
			if err != nil {
				s.logger.Warn("failed to get volume size",
					zap.String("volume", vol.Name),
					zap.Error(err),
				)
				continue
			}
			volumeInfos = append(volumeInfos, &VolumeInfo{
				Name:       info.Name,
				Driver:     info.Driver,
				Mountpoint: info.Mountpoint,
				Labels:     info.Labels,
				Size:       info.Size,
			})
		}
		c.JSON(http.StatusOK, volumeInfos)
	} else {
		c.JSON(http.StatusOK, volumes)
	}
}

// GetVolume returns detailed volume information
func (s *Server) GetVolume(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()

	volumeName := c.Param("name")

	info, err := s.docker.GetVolumeInfo(ctx, volumeName)
	if err != nil {
		s.logger.Error("failed to get volume", zap.String("name", volumeName), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, info)
}

// CreateVolume creates a new volume
func (s *Server) CreateVolume(c *gin.Context) {
	var req struct {
		Name    string            `json:"name" binding:"required"`
		Driver  string            `json:"driver"`
		Labels  map[string]string `json:"labels"`
		Options map[string]string `json:"options"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	vol, err := s.docker.CreateVolume(ctx, req.Name, req.Labels, req.Options)
	if err != nil {
		s.logger.Error("failed to create volume", zap.String("name", req.Name), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	s.hub.Broadcast([]byte(`{"type":"resource_update","resource":"volumes"}`))

	c.JSON(http.StatusCreated, gin.H{
		"status": "created",
		"name":   vol.Name,
		"driver": vol.Driver,
	})
}

// RemoveVolume removes a volume
func (s *Server) RemoveVolume(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	volumeName := c.Param("name")
	force := c.Query("force") == "true"

	if err := s.docker.RemoveVolume(ctx, volumeName, force); err != nil {
		s.logger.Error("failed to remove volume", zap.String("name", volumeName), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	s.hub.Broadcast([]byte(`{"type":"resource_update","resource":"volumes"}`))

	c.JSON(http.StatusOK, gin.H{"status": "removed", "name": volumeName})
}

// ListNetworks returns all networks
func (s *Server) ListNetworks(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	networks, err := s.docker.ListNetworks(ctx)
	if err != nil {
		s.logger.Error("failed to list networks", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, networks)
}

// GetNetwork returns detailed network information
func (s *Server) GetNetwork(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	networkID := c.Param("id")

	info, err := s.docker.ExportNetwork(ctx, networkID)
	if err != nil {
		s.logger.Error("failed to get network", zap.String("id", networkID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, info)
}

// CreateNetwork creates a new network
func (s *Server) CreateNetwork(c *gin.Context) {
	var req struct {
		Name       string            `json:"name" binding:"required"`
		Driver     string            `json:"driver"`
		Internal   bool              `json:"internal"`
		Attachable bool              `json:"attachable"`
		Labels     map[string]string `json:"labels"`
		Options    map[string]string `json:"options"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	// Use default driver if not specified
	driver := req.Driver
	if driver == "" {
		driver = "bridge"
	}

	networkInfo := &docker.NetworkInfo{
		Name:       req.Name,
		Driver:     driver,
		Internal:   req.Internal,
		Attachable: req.Attachable,
		Labels:     req.Labels,
		Options:    req.Options,
	}

	networkID, err := s.docker.CreateNetwork(ctx, networkInfo, req.Name)
	if err != nil {
		s.logger.Error("failed to create network", zap.String("name", req.Name), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	s.hub.Broadcast([]byte(`{"type":"resource_update","resource":"networks"}`))

	c.JSON(http.StatusCreated, gin.H{
		"status":     "created",
		"name":       req.Name,
		"network_id": networkID,
	})
}

// RemoveNetwork removes a network
func (s *Server) RemoveNetwork(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	networkID := c.Param("id")

	if err := s.docker.RemoveNetwork(ctx, networkID); err != nil {
		s.logger.Error("failed to remove network", zap.String("id", networkID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	s.hub.Broadcast([]byte(`{"type":"resource_update","resource":"networks"}`))

	c.JSON(http.StatusOK, gin.H{"status": "removed", "network_id": networkID})
}

// ListPeers returns all connected peers
func (s *Server) ListPeers(c *gin.Context) {
	if s.pairing == nil {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}

	peers := s.pairing.ListTrustedPeers()
	c.JSON(http.StatusOK, peers)
}

// GeneratePairingCode generates a pairing code for peer connection
func (s *Server) GeneratePairingCode(c *gin.Context) {
	if s.pairing == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "pairing manager not initialized",
		})
		return
	}

	code, err := s.pairing.GeneratePairingCode()
	if err != nil {
		s.logger.Error("failed to generate pairing code", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":       code,
		"expires_in": 300, // 5 minutes
		"message":    "Share this code with the peer to establish connection",
	})
}

// ConnectWithCode connects to a peer using a pairing code
func (s *Server) ConnectWithCode(c *gin.Context) {
	var req struct {
		Code        string `json:"code" binding:"required"`
		PeerAddress string `json:"peer_address" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if s.pairing == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "pairing manager not initialized",
		})
		return
	}

	// Get pairing message for exchange
	msg, err := s.pairing.GetPairingMessage(req.Code)
	if err != nil {
		s.logger.Error("failed to get pairing message", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Return pairing message for peer exchange
	c.JSON(http.StatusOK, gin.H{
		"status":     "initiated",
		"message":    "Pairing initiated, send this to peer",
		"public_key": msg.PublicKey,
		"verifier":   msg.CodeVerifier,
	})
}

// StartMigration starts a migration job
func (s *Server) StartMigration(c *gin.Context) {
	var req struct {
		PeerID     string   `json:"peer_id" binding:"required"`
		Mode       string   `json:"mode"`      // copy or move
		Strategy   string   `json:"strategy"`  // cold, warm, snapshot
		Containers []string `json:"containers"`
		Images     []string `json:"images"`
		Volumes    []string `json:"volumes"`
		Networks   []string `json:"networks"`
		DryRun     bool     `json:"dry_run"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if s.migration == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "migration engine not initialized",
		})
		return
	}

	// Build resource refs
	var resources []migration.ResourceRef
	for _, id := range req.Containers {
		resources = append(resources, migration.ResourceRef{
			Type: "container",
			ID:   id,
			Name: id,
		})
	}
	for _, id := range req.Images {
		resources = append(resources, migration.ResourceRef{
			Type: "image",
			ID:   id,
			Name: id,
		})
	}
	for _, name := range req.Volumes {
		resources = append(resources, migration.ResourceRef{
			Type: "volume",
			ID:   name,
			Name: name,
		})
	}
	for _, id := range req.Networks {
		resources = append(resources, migration.ResourceRef{
			Type: "network",
			ID:   id,
			Name: id,
		})
	}

	// Create migration job
	job := &migration.MigrationJob{
		ID:        generateJobID(),
		PeerID:    req.PeerID,
		Mode:      migration.MigrationMode(req.Mode),
		Strategy:  migration.MigrationStrategy(req.Strategy),
		Resources: resources,
	}

	// Handle dry-run
	if req.DryRun {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
		defer cancel()

		result, err := s.migration.GenerateDryRun(ctx, job)
		if err != nil {
			s.logger.Error("dry-run failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, result)
		return
	}

	// Start actual migration
	if err := s.migration.StartMigration(c.Request.Context(), job); err != nil {
		s.logger.Error("failed to start migration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"job_id": job.ID,
		"status": "started",
		"message": "Migration started, use WebSocket for real-time progress",
	})
}

// generateJobID creates a unique job identifier
func generateJobID() string {
	return fmt.Sprintf("mig_%d", time.Now().UnixNano())
}

// GetMigrationStatus returns the status of a migration job
func (s *Server) GetMigrationStatus(c *gin.Context) {
	migrationID := c.Param("id")

	if s.migration == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "migration engine not initialized",
		})
		return
	}

	job, err := s.migration.GetStatus(migrationID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, job)
}

// CancelMigration cancels a running migration
func (s *Server) CancelMigration(c *gin.Context) {
	migrationID := c.Param("id")

	if s.migration == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "migration engine not initialized",
		})
		return
	}

	if err := s.migration.CancelMigration(migrationID); err != nil {
		s.logger.Error("failed to cancel migration",
			zap.String("job_id", migrationID),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "cancelled",
		"message": "Migration cancelled and rollback initiated",
	})
}

// GetMigrationHistory returns past migrations
func (s *Server) GetMigrationHistory(c *gin.Context) {
	// TODO: Implement migration history
	c.JSON(http.StatusOK, gin.H{
		"migrations": []interface{}{},
		"count":      0,
	})
}

// ListComposeStacks returns all detected compose stacks
func (s *Server) ListComposeStacks(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	stacks, err := s.docker.DetectComposeStacks(ctx)
	if err != nil {
		s.logger.Error("failed to detect compose stacks", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stacks)
}

// GetComposeStack returns details of a specific compose stack
func (s *Server) GetComposeStack(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	stackName := c.Param("name")

	stacks, err := s.docker.DetectComposeStacks(ctx)
	if err != nil {
		s.logger.Error("failed to detect compose stacks", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for _, stack := range stacks {
		if stack.Name == stackName {
			c.JSON(http.StatusOK, stack)
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "compose stack not found"})
}

// ValidateCompose validates a Docker Compose file
func (s *Server) ValidateCompose(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Minute)
	defer cancel()

	project, err := s.docker.LoadComposeFile(ctx, req.Path)
	if err != nil {
		s.logger.Error("failed to load compose file", zap.String("path", req.Path), zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.docker.ValidateComposeProject(ctx, project); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"valid":  false,
			"errors": []string{err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"valid":   true,
		"project": project.Name,
		"summary": gin.H{
			"services": len(project.Services),
			"networks": len(project.Networks),
			"volumes":  len(project.Volumes),
		},
	})
}

// ExportCompose exports all resources from a Compose project
func (s *Server) ExportCompose(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()

	project, err := s.docker.LoadComposeFile(ctx, req.Path)
	if err != nil {
		s.logger.Error("failed to load compose file", zap.String("path", req.Path), zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resources, err := s.docker.ExportComposeResources(ctx, project)
	if err != nil {
		s.logger.Error("failed to export compose resources", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"project":   project.Name,
		"resources": resources,
	})
}

// VolumeInfo for API response
type VolumeInfo struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Labels     map[string]string `json:"labels"`
	Size       int64             `json:"size"`
}

// GetResourceCounts returns counts of all Docker resources
func (s *Server) GetResourceCounts(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	counts := gin.H{
		"containers":        0,
		"images":            0,
		"volumes":           0,
		"networks":          0,
		"composeStacks":     0,
		"runningContainers": 0,
		"totalImageSize":    int64(0),
		"totalVolumeSize":   int64(0),
	}

	// Get container count and running count
	containers, err := s.docker.ListContainers(ctx, true)
	if err != nil {
		s.logger.Warn("failed to count containers", zap.Error(err))
	} else {
		counts["containers"] = len(containers)
		runningCount := 0
		for _, c := range containers {
			if c.State == "running" {
				runningCount++
			}
		}
		counts["runningContainers"] = runningCount
	}

	// Get image count and total size
	images, err := s.docker.ListImages(ctx)
	if err != nil {
		s.logger.Warn("failed to count images", zap.Error(err))
	} else {
		counts["images"] = len(images)
		var totalSize int64
		for _, img := range images {
			totalSize += img.Size
		}
		counts["totalImageSize"] = totalSize
	}

	// Get volume count
	volumes, err := s.docker.ListVolumes(ctx)
	if err != nil {
		s.logger.Warn("failed to count volumes", zap.Error(err))
	} else {
		counts["volumes"] = len(volumes)
	}

	// Get network count
	networks, err := s.docker.ListNetworks(ctx)
	if err != nil {
		s.logger.Warn("failed to count networks", zap.Error(err))
	} else {
		counts["networks"] = len(networks)
	}

	// Get compose stacks count
	stacks, err := s.docker.DetectComposeStacks(ctx)
	if err != nil {
		s.logger.Warn("failed to count compose stacks", zap.Error(err))
	} else {
		counts["composeStacks"] = len(stacks)
	}

	c.JSON(http.StatusOK, counts)
}
