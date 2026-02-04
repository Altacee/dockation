package master

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	pb "github.com/artemis/docker-migrate/proto"
)

// MigrationResponse is the API response for a migration
type MigrationResponse struct {
	ID               string     `json:"id"`
	SourceWorkerID   string     `json:"source_worker_id"`
	TargetWorkerID   string     `json:"target_worker_id"`
	Status           string     `json:"status"`
	Phase            string     `json:"phase"`
	Progress         float32    `json:"progress"`
	BytesTransferred int64      `json:"bytes_transferred"`
	TotalBytes       int64      `json:"total_bytes"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	Error            string     `json:"error,omitempty"`
	ContainerIDs     []string   `json:"container_ids,omitempty"`
	ImageIDs         []string   `json:"image_ids,omitempty"`
	VolumeNames      []string   `json:"volume_names,omitempty"`
	NetworkIDs       []string   `json:"network_ids,omitempty"`
}

// StartMigrationRequest is the request body for starting a migration
type StartMigrationRequest struct {
	SourceWorkerID string   `json:"source_worker_id" binding:"required"`
	TargetWorkerID string   `json:"target_worker_id" binding:"required"`
	ContainerIDs   []string `json:"container_ids"`
	ImageIDs       []string `json:"image_ids"`
	VolumeNames    []string `json:"volume_names"`
	NetworkIDs     []string `json:"network_ids"`
	Mode           string   `json:"mode"`     // cold, warm, live
	Strategy       string   `json:"strategy"` // full, incremental, snapshot
}

// RegisterMigrationRoutes registers migration API routes
func (m *Master) RegisterMigrationRoutes(rg *gin.RouterGroup) {
	rg.POST("/migrations", m.startMigration)
	rg.GET("/migrations", m.listMigrations)
	rg.GET("/migrations/:id", m.getMigration)
	rg.POST("/migrations/:id/cancel", m.cancelMigration)
}

func (m *Master) startMigration(c *gin.Context) {
	var req StartMigrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate at least one resource type is specified
	if len(req.ContainerIDs) == 0 && len(req.ImageIDs) == 0 &&
		len(req.VolumeNames) == 0 && len(req.NetworkIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one resource must be specified"})
		return
	}

	// Parse mode and strategy
	mode := pb.MigrationMode_MIGRATION_MODE_COLD
	switch req.Mode {
	case "warm":
		mode = pb.MigrationMode_MIGRATION_MODE_WARM
	case "live":
		mode = pb.MigrationMode_MIGRATION_MODE_LIVE
	}

	strategy := pb.MigrationStrategy_MIGRATION_STRATEGY_FULL
	switch req.Strategy {
	case "incremental":
		strategy = pb.MigrationStrategy_MIGRATION_STRATEGY_INCREMENTAL
	case "snapshot":
		strategy = pb.MigrationStrategy_MIGRATION_STRATEGY_SNAPSHOT
	}

	job, err := m.orchestrator.StartMigration(c.Request.Context(), &MigrationRequest{
		SourceWorkerID: req.SourceWorkerID,
		TargetWorkerID: req.TargetWorkerID,
		ContainerIDs:   req.ContainerIDs,
		ImageIDs:       req.ImageIDs,
		VolumeNames:    req.VolumeNames,
		NetworkIDs:     req.NetworkIDs,
		Mode:           mode,
		Strategy:       strategy,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, migrationToResponse(job))
}

func (m *Master) listMigrations(c *gin.Context) {
	jobs := m.orchestrator.ListMigrations()

	response := make([]MigrationResponse, 0, len(jobs))
	for _, j := range jobs {
		response = append(response, migrationToResponse(j))
	}

	c.JSON(http.StatusOK, gin.H{"migrations": response})
}

func (m *Master) getMigration(c *gin.Context) {
	migrationID := c.Param("id")

	job, ok := m.orchestrator.GetMigration(migrationID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "migration not found"})
		return
	}

	c.JSON(http.StatusOK, migrationToResponse(job))
}

func (m *Master) cancelMigration(c *gin.Context) {
	migrationID := c.Param("id")

	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)

	if req.Reason == "" {
		req.Reason = "cancelled by user"
	}

	if err := m.orchestrator.CancelMigration(migrationID, req.Reason); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "migration cancelled"})
}

func migrationToResponse(j *MigrationJob) MigrationResponse {
	j.mu.RLock()
	defer j.mu.RUnlock()

	resp := MigrationResponse{
		ID:               j.ID,
		SourceWorkerID:   j.SourceWorkerID,
		TargetWorkerID:   j.TargetWorkerID,
		Status:           string(j.Status),
		Phase:            j.Phase.String(),
		Progress:         j.Progress,
		BytesTransferred: j.BytesTransferred,
		TotalBytes:       j.TotalBytes,
		StartedAt:        j.StartedAt,
		Error:            j.Error,
		ContainerIDs:     j.ContainerIDs,
		ImageIDs:         j.ImageIDs,
		VolumeNames:      j.VolumeNames,
		NetworkIDs:       j.NetworkIDs,
	}

	if !j.CompletedAt.IsZero() {
		resp.CompletedAt = &j.CompletedAt
	}

	return resp
}
