package observability

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// HealthStatus represents the health state of a component
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// ComponentHealth tracks the health of a single component
type ComponentHealth struct {
	Status    HealthStatus `json:"status"`
	Message   string       `json:"message,omitempty"`
	LastCheck time.Time    `json:"last_check"`
}

// HealthChecker manages health checks for all components
type HealthChecker struct {
	mu         sync.RWMutex
	components map[string]*ComponentHealth
	checks     map[string]HealthCheckFunc
}

// HealthCheckFunc is a function that checks the health of a component
type HealthCheckFunc func(ctx context.Context) error

// NewHealthChecker creates a new health checker
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		components: make(map[string]*ComponentHealth),
		checks:     make(map[string]HealthCheckFunc),
	}
}

// RegisterCheck registers a health check function for a component
func (hc *HealthChecker) RegisterCheck(name string, check HealthCheckFunc) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.checks[name] = check
	hc.components[name] = &ComponentHealth{
		Status:    HealthStatusHealthy,
		LastCheck: time.Now(),
	}
}

// RunChecks executes all registered health checks
func (hc *HealthChecker) RunChecks(ctx context.Context) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	for name, check := range hc.checks {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := check(checkCtx)
		cancel()

		health := &ComponentHealth{
			LastCheck: time.Now(),
		}

		if err != nil {
			health.Status = HealthStatusUnhealthy
			health.Message = err.Error()
		} else {
			health.Status = HealthStatusHealthy
		}

		hc.components[name] = health
	}
}

// GetHealth returns the current health status
func (hc *HealthChecker) GetHealth() map[string]*ComponentHealth {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make(map[string]*ComponentHealth)
	for name, health := range hc.components {
		result[name] = health
	}
	return result
}

// IsHealthy returns true if all components are healthy
func (hc *HealthChecker) IsHealthy() bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	for _, health := range hc.components {
		if health.Status == HealthStatusUnhealthy {
			return false
		}
	}
	return true
}

// IsReady returns true if critical components are healthy
func (hc *HealthChecker) IsReady() bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	// Check docker component specifically for readiness
	if docker, ok := hc.components["docker"]; ok {
		if docker.Status == HealthStatusUnhealthy {
			return false
		}
	}
	return true
}

// HealthHandler returns a gin handler for the /health endpoint
func (hc *HealthChecker) HealthHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		health := hc.GetHealth()
		status := http.StatusOK

		if !hc.IsHealthy() {
			status = http.StatusServiceUnavailable
		}

		c.JSON(status, gin.H{
			"status":     hc.overallStatus(),
			"components": health,
			"timestamp":  time.Now(),
		})
	}
}

// ReadyHandler returns a gin handler for the /ready endpoint
func (hc *HealthChecker) ReadyHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !hc.IsReady() {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":    "not_ready",
				"timestamp": time.Now(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":    "ready",
			"timestamp": time.Now(),
		})
	}
}

func (hc *HealthChecker) overallStatus() HealthStatus {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	hasUnhealthy := false
	hasDegraded := false

	for _, health := range hc.components {
		switch health.Status {
		case HealthStatusUnhealthy:
			hasUnhealthy = true
		case HealthStatusDegraded:
			hasDegraded = true
		}
	}

	if hasUnhealthy {
		return HealthStatusUnhealthy
	}
	if hasDegraded {
		return HealthStatusDegraded
	}
	return HealthStatusHealthy
}

// StartPeriodicChecks starts running health checks periodically
func (hc *HealthChecker) StartPeriodicChecks(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hc.RunChecks(ctx)
		}
	}
}

// DockerHealthCheck creates a health check function for Docker daemon
func DockerHealthCheck(pingFunc func(context.Context) error) HealthCheckFunc {
	return func(ctx context.Context) error {
		if err := pingFunc(ctx); err != nil {
			return fmt.Errorf("docker daemon unreachable: %w", err)
		}
		return nil
	}
}
