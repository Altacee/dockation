package master

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// WorkerResponse is the API response for a worker
type WorkerResponse struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Hostname       string            `json:"hostname"`
	GRPCAddress    string            `json:"grpc_address"`
	Labels         map[string]string `json:"labels"`
	Version        string            `json:"version"`
	Status         string            `json:"status"`
	Online         bool              `json:"online"`
	RegisteredAt   time.Time         `json:"registered_at"`
	LastHeartbeat  time.Time         `json:"last_heartbeat"`
	ContainerCount int               `json:"container_count"`
	ImageCount     int               `json:"image_count"`
	VolumeCount    int               `json:"volume_count"`
	NetworkCount   int               `json:"network_count"`
}

// RegisterWorkerRoutes registers worker management routes
func (m *Master) RegisterWorkerRoutes(rg *gin.RouterGroup) {
	rg.GET("/workers", m.listWorkers)
	rg.GET("/workers/:id", m.getWorker)
	rg.GET("/workers/:id/resources", m.getWorkerResources)
	rg.DELETE("/workers/:id", m.removeWorker)
	rg.GET("/enrollment-token", m.getEnrollmentToken)
	rg.POST("/enrollment-token/regenerate", m.regenerateEnrollmentToken)
}

func (m *Master) listWorkers(c *gin.Context) {
	workers := m.registry.List()

	response := make([]WorkerResponse, 0, len(workers))
	for _, w := range workers {
		response = append(response, workerToResponse(w, m.registry.IsOnline(w.ID)))
	}

	c.JSON(http.StatusOK, gin.H{"workers": response})
}

func (m *Master) getWorker(c *gin.Context) {
	workerID := c.Param("id")

	w, ok := m.registry.Get(workerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "worker not found"})
		return
	}

	c.JSON(http.StatusOK, workerToResponse(w, m.registry.IsOnline(workerID)))
}

func (m *Master) getWorkerResources(c *gin.Context) {
	workerID := c.Param("id")

	w, ok := m.registry.Get(workerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "worker not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"worker_id":  workerID,
		"containers": w.Containers,
		"images":     w.Images,
		"volumes":    w.Volumes,
		"networks":   w.Networks,
		"updated_at": w.LastInventory,
	})
}

func (m *Master) removeWorker(c *gin.Context) {
	workerID := c.Param("id")

	if _, ok := m.registry.Get(workerID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "worker not found"})
		return
	}

	m.registry.Unregister(workerID)
	c.JSON(http.StatusOK, gin.H{"message": "worker removed"})
}

func (m *Master) getEnrollmentToken(c *gin.Context) {
	if m.config.Master == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "master config not set"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"enrollment_token": m.config.Master.EnrollmentToken,
	})
}

func (m *Master) regenerateEnrollmentToken(c *gin.Context) {
	if m.config.Master == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "master config not set"})
		return
	}

	newToken := generateToken(32)
	m.config.Master.EnrollmentToken = newToken

	m.logger.Info("enrollment token regenerated")

	c.JSON(http.StatusOK, gin.H{
		"enrollment_token": newToken,
	})
}

func workerToResponse(w *WorkerInfo, online bool) WorkerResponse {
	return WorkerResponse{
		ID:             w.ID,
		Name:           w.Name,
		Hostname:       w.Hostname,
		GRPCAddress:    w.GRPCAddress,
		Labels:         w.Labels,
		Version:        w.Version,
		Status:         w.Status.String(),
		Online:         online,
		RegisteredAt:   w.RegisteredAt,
		LastHeartbeat:  w.LastHeartbeat,
		ContainerCount: len(w.Containers),
		ImageCount:     len(w.Images),
		VolumeCount:    len(w.Volumes),
		NetworkCount:   len(w.Networks),
	}
}
