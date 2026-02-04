package server

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/artemis/docker-migrate/internal/config"
	"github.com/artemis/docker-migrate/internal/docker"
	"github.com/artemis/docker-migrate/internal/master"
	"github.com/artemis/docker-migrate/internal/migration"
	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/artemis/docker-migrate/internal/peer"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

//go:embed dist/*
var webUI embed.FS

// Server represents the HTTP server
type Server struct {
	config         *config.Config
	docker         *docker.Client
	logger         *observability.Logger
	health         *observability.HealthChecker
	migration      *migration.Engine
	pairing        *peer.PairingManager
	discovery      *peer.PeerDiscovery
	metrics        *observability.Metrics
	hub            *Hub
	router         *gin.Engine
	master         *master.Master // Set when running in master mode
}

// NewServer creates a new HTTP server
func NewServer(
	cfg *config.Config,
	dockerClient *docker.Client,
	logger *observability.Logger,
	healthChecker *observability.HealthChecker,
) *Server {
	// Set gin mode based on log level
	if cfg.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	s := &Server{
		config: cfg,
		docker: dockerClient,
		logger: logger,
		health: healthChecker,
		hub:    NewHub(logger),
	}

	s.setupRouter()
	return s
}

// NewServerWithDeps creates a new HTTP server with all dependencies wired
func NewServerWithDeps(
	cfg *config.Config,
	dockerClient *docker.Client,
	migrationEngine *migration.Engine,
	pairingManager *peer.PairingManager,
	peerDiscovery *peer.PeerDiscovery,
	healthChecker *observability.HealthChecker,
	metrics *observability.Metrics,
	logger *observability.Logger,
) *Server {
	// Set gin mode based on log level
	if cfg.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	s := &Server{
		config:    cfg,
		docker:    dockerClient,
		logger:    logger,
		health:    healthChecker,
		migration: migrationEngine,
		pairing:   pairingManager,
		discovery: peerDiscovery,
		metrics:   metrics,
		hub:       NewHub(logger),
	}

	s.setupRouter()
	return s
}

// setupRouter configures all routes
func (s *Server) setupRouter() {
	r := gin.New()

	// Middleware
	r.Use(gin.Recovery())
	r.Use(s.loggingMiddleware())
	r.Use(s.corsMiddleware())

	// Health endpoints (no auth required)
	r.GET("/health", s.health.HealthHandler())
	r.GET("/ready", s.health.ReadyHandler())

	// Metrics endpoint (no auth required)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// API routes
	api := r.Group("/api")
	{
		// Resource counts (for dashboard)
		api.GET("/resources/counts", s.GetResourceCounts)

		// Container management
		api.GET("/containers", s.ListContainers)
		api.GET("/containers/:id", s.GetContainer)
		api.POST("/containers/:id/start", s.StartContainer)
		api.POST("/containers/:id/stop", s.StopContainer)
		api.POST("/containers/:id/restart", s.RestartContainer)
		api.DELETE("/containers/:id", s.RemoveContainer)
		api.GET("/containers/:id/logs", s.GetContainerLogs)

		// Image management
		api.GET("/images", s.ListImages)
		api.GET("/images/:id", s.GetImage)
		api.POST("/images/pull", s.PullImage)
		api.DELETE("/images/:id", s.RemoveImage)

		// Volume management
		api.GET("/volumes", s.ListVolumes)
		api.GET("/volumes/:name", s.GetVolume)
		api.POST("/volumes", s.CreateVolume)
		api.DELETE("/volumes/:name", s.RemoveVolume)

		// Network management
		api.GET("/networks", s.ListNetworks)
		api.GET("/networks/:id", s.GetNetwork)
		api.POST("/networks", s.CreateNetwork)
		api.DELETE("/networks/:id", s.RemoveNetwork)

		// Peer management
		api.GET("/peers", s.ListPeers)
		api.POST("/pair/generate", s.GeneratePairingCode)
		api.POST("/pair/connect", s.ConnectWithCode)

		// Migration operations
		api.POST("/migrate", s.StartMigration)
		api.GET("/migrate/:id/status", s.GetMigrationStatus)
		api.POST("/migrate/:id/cancel", s.CancelMigration)
		api.GET("/migrate/history", s.GetMigrationHistory)

		// Compose operations
		api.GET("/compose", s.ListComposeStacks)
		api.GET("/compose/:name", s.GetComposeStack)
		api.POST("/compose/validate", s.ValidateCompose)
		api.POST("/compose/export", s.ExportCompose)
	}

	// WebSocket endpoints
	r.GET("/ws", s.HandleWebSocket)
	r.GET("/ws/containers/:id/logs", s.HandleContainerLogs)

	// Serve embedded web UI
	s.setupStaticFiles(r)

	s.router = r
}

// setupStaticFiles configures serving of embedded web UI
func (s *Server) setupStaticFiles(r *gin.Engine) {
	// Try to serve embedded files
	distFS, err := fs.Sub(webUI, "dist")
	if err != nil {
		s.logger.Warn("web UI not embedded, will not serve static files")
		// Serve a simple message instead
		r.GET("/", func(c *gin.Context) {
			c.String(http.StatusOK, "docker-migrate API server running. Web UI not available.")
		})
		return
	}

	// Serve index.html for root and all non-API routes (SPA support)
	r.NoRoute(func(c *gin.Context) {
		// Check if this is an API route
		if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
			c.JSON(http.StatusNotFound, gin.H{"error": "endpoint not found"})
			return
		}

		// Serve static files or index.html
		c.FileFromFS(c.Request.URL.Path, http.FS(distFS))
	})

	r.StaticFS("/assets", http.FS(distFS))
}

// loggingMiddleware logs HTTP requests
func (s *Server) loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Don't log health check spam
		if c.Request.URL.Path == "/health" || c.Request.URL.Path == "/ready" {
			c.Next()
			return
		}

		c.Next()

		// Log after request completes
		s.logger.InfoRedacted("http request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.String("ip", c.ClientIP()),
		)
	}
}

// corsMiddleware handles CORS
func (s *Server) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	// Start WebSocket hub
	go s.hub.Run()

	s.logger.Info("starting HTTP server",
		zap.String("addr", s.config.HTTPAddr),
	)

	if err := s.router.Run(s.config.HTTPAddr); err != nil {
		return err
	}

	return nil
}

// Stop gracefully stops the server
func (s *Server) Stop() error {
	s.logger.Info("stopping HTTP server")
	s.hub.Stop()
	return nil
}

// Broadcast sends a message to all connected WebSocket clients
func (s *Server) Broadcast(message []byte) {
	s.hub.Broadcast(message)
}

// SetMaster sets the master instance for master-mode API routes
func (s *Server) SetMaster(m *master.Master) {
	s.master = m
	// Register master-specific routes
	api := s.router.Group("/api")
	m.RegisterWorkerRoutes(api)
	m.RegisterMigrationRoutes(api)
}

// GetRouter returns the gin router for direct route registration
func (s *Server) GetRouter() *gin.Engine {
	return s.router
}
