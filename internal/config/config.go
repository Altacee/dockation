package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/observability"
)

// Role constants
const (
	RoleMaster = "master"
	RoleWorker = "worker"
	RoleP2P    = "" // Default P2P mode (empty string)
)

// Config holds all application configuration
type Config struct {
	// Server configuration
	HTTPAddr string `json:"http_addr"`
	GRPCAddr string `json:"grpc_addr"`

	// Docker configuration
	DockerHost string `json:"docker_host"`

	// Security configuration
	TLSEnabled bool   `json:"tls_enabled"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`

	// Transfer configuration
	ChunkSize        int           `json:"chunk_size"`
	MaxConcurrent    int           `json:"max_concurrent"`
	TransferTimeout  time.Duration `json:"transfer_timeout"`
	VerifyChecksums  bool          `json:"verify_checksums"`
	CompressionLevel int           `json:"compression_level"`

	// Retry configuration
	MaxRetries      int           `json:"max_retries"`
	RetryBackoff    time.Duration `json:"retry_backoff"`
	RetryMaxBackoff time.Duration `json:"retry_max_backoff"`

	// Logging configuration
	LogLevel string `json:"log_level"`

	// Data directory for certificates and state
	DataDir string `json:"data_dir"`

	// Trusted peers
	TrustedPeers map[string]*TrustedPeer `json:"trusted_peers"`

	// Role configuration (master, worker, or empty for P2P mode)
	Role   string        `json:"role,omitempty"`
	Master *MasterConfig `json:"master,omitempty"`
	Worker *WorkerConfig `json:"worker,omitempty"`

	mu sync.RWMutex
}

// MasterConfig holds master-specific configuration
type MasterConfig struct {
	// EnrollmentToken is required for workers to register
	EnrollmentToken string `json:"enrollment_token"`

	// WorkerTimeout is how long to wait before marking worker as offline
	WorkerTimeout time.Duration `json:"worker_timeout"`

	// HeartbeatInterval is how often workers should send heartbeats
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`

	// InventoryInterval is how often workers should report resource inventory
	InventoryInterval time.Duration `json:"inventory_interval"`

	// MaxWorkers is the maximum number of workers allowed (0 = unlimited)
	MaxWorkers int `json:"max_workers"`
}

// WorkerConfig holds worker-specific configuration
type WorkerConfig struct {
	// MasterURL is the gRPC address of the master node
	MasterURL string `json:"master_url"`

	// AuthToken is received after registration for authenticating subsequent requests
	AuthToken string `json:"auth_token"`

	// WorkerID is assigned by master after registration
	WorkerID string `json:"worker_id"`

	// Name is the human-readable worker name
	Name string `json:"name"`

	// Labels are key-value pairs for filtering workers
	Labels map[string]string `json:"labels"`

	// ReconnectInterval is the base interval between reconnection attempts
	ReconnectInterval time.Duration `json:"reconnect_interval"`

	// MaxReconnectInterval is the maximum backoff for reconnection attempts
	MaxReconnectInterval time.Duration `json:"max_reconnect_interval"`
}

// DefaultMasterConfig returns default master configuration
func DefaultMasterConfig() *MasterConfig {
	return &MasterConfig{
		EnrollmentToken:   "", // Must be set or generated
		WorkerTimeout:     30 * time.Second,
		HeartbeatInterval: 10 * time.Second,
		InventoryInterval: 60 * time.Second,
		MaxWorkers:        0, // Unlimited
	}
}

// DefaultWorkerConfig returns default worker configuration
func DefaultWorkerConfig() *WorkerConfig {
	return &WorkerConfig{
		MasterURL:            "",
		AuthToken:            "",
		WorkerID:             "",
		Name:                 "",
		Labels:               make(map[string]string),
		ReconnectInterval:    5 * time.Second,
		MaxReconnectInterval: 5 * time.Minute,
	}
}

// TrustedPeer represents a peer that has been paired
type TrustedPeer struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint"`
	Address     string    `json:"address"`
	AddedAt     time.Time `json:"added_at"`
	LastSeen    time.Time `json:"last_seen"`
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		HTTPAddr:         ":8080",
		GRPCAddr:         ":9090",
		DockerHost:       "", // Use default Docker socket
		TLSEnabled:       true,
		ChunkSize:        1024 * 1024 * 4, // 4MB chunks
		MaxConcurrent:    4,
		TransferTimeout:  time.Hour,
		VerifyChecksums:  true,
		CompressionLevel: 3, // zstd default
		MaxRetries:       5,
		RetryBackoff:     time.Second,
		RetryMaxBackoff:  time.Minute,
		LogLevel:         "info",
		DataDir:          "",  // Will use ~/.docker-migrate by default
		TrustedPeers:     make(map[string]*TrustedPeer),
	}
}

// LoadConfig loads configuration from a file or returns default config
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		// Try default locations
		homeDir, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(homeDir, ".docker-migrate", "config.json")
		}
	}

	// If file doesn't exist, return default config
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply defaults for missing fields
	applyDefaults(&cfg)

	return &cfg, nil
}

// Save saves the configuration to a file
func (c *Config) Save(path string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if path == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(homeDir, ".docker-migrate", "config.json")
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal with indentation for readability
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to temporary file first, then atomic rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename config file: %w", err)
	}

	return nil
}

// AddTrustedPeer adds a peer to the trusted peers list
func (c *Config) AddTrustedPeer(peer *TrustedPeer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TrustedPeers[peer.ID] = peer
}

// RemoveTrustedPeer removes a peer from the trusted peers list
func (c *Config) RemoveTrustedPeer(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.TrustedPeers, id)
}

// GetTrustedPeer retrieves a trusted peer by ID
func (c *Config) GetTrustedPeer(id string) (*TrustedPeer, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	peer, ok := c.TrustedPeers[id]
	return peer, ok
}

// UpdatePeerLastSeen updates the last seen timestamp for a peer
func (c *Config) UpdatePeerLastSeen(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if peer, ok := c.TrustedPeers[id]; ok {
		peer.LastSeen = time.Now()
	}
}

// ListTrustedPeers returns a list of all trusted peers
func (c *Config) ListTrustedPeers() []*TrustedPeer {
	c.mu.RLock()
	defer c.mu.RUnlock()

	peers := make([]*TrustedPeer, 0, len(c.TrustedPeers))
	for _, peer := range c.TrustedPeers {
		peers = append(peers, peer)
	}
	return peers
}

// Redact returns a redacted copy of the config for logging
func (c *Config) Redact() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"http_addr":         c.HTTPAddr,
		"grpc_addr":         c.GRPCAddr,
		"docker_host":       observability.RedactString(c.DockerHost),
		"tls_enabled":       c.TLSEnabled,
		"cert_file":         c.CertFile,
		"key_file":          "***REDACTED***",
		"chunk_size":        c.ChunkSize,
		"max_concurrent":    c.MaxConcurrent,
		"transfer_timeout":  c.TransferTimeout,
		"verify_checksums":  c.VerifyChecksums,
		"compression_level": c.CompressionLevel,
		"max_retries":       c.MaxRetries,
		"log_level":         c.LogLevel,
		"trusted_peers":     len(c.TrustedPeers),
	}
}

func applyDefaults(cfg *Config) {
	defaults := DefaultConfig()

	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = defaults.HTTPAddr
	}
	if cfg.GRPCAddr == "" {
		cfg.GRPCAddr = defaults.GRPCAddr
	}
	if cfg.ChunkSize == 0 {
		cfg.ChunkSize = defaults.ChunkSize
	}
	if cfg.MaxConcurrent == 0 {
		cfg.MaxConcurrent = defaults.MaxConcurrent
	}
	if cfg.TransferTimeout == 0 {
		cfg.TransferTimeout = defaults.TransferTimeout
	}
	if cfg.CompressionLevel == 0 {
		cfg.CompressionLevel = defaults.CompressionLevel
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = defaults.MaxRetries
	}
	if cfg.RetryBackoff == 0 {
		cfg.RetryBackoff = defaults.RetryBackoff
	}
	if cfg.RetryMaxBackoff == 0 {
		cfg.RetryMaxBackoff = defaults.RetryMaxBackoff
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = defaults.LogLevel
	}
	if cfg.TrustedPeers == nil {
		cfg.TrustedPeers = make(map[string]*TrustedPeer)
	}

	// Apply role-specific defaults
	if cfg.Role == RoleMaster && cfg.Master == nil {
		cfg.Master = DefaultMasterConfig()
	}
	if cfg.Role == RoleWorker && cfg.Worker == nil {
		cfg.Worker = DefaultWorkerConfig()
	}
}

// IsMaster returns true if running in master mode
func (c *Config) IsMaster() bool {
	return c.Role == RoleMaster
}

// IsWorker returns true if running in worker mode
func (c *Config) IsWorker() bool {
	return c.Role == RoleWorker
}

// IsP2P returns true if running in P2P mode (default)
func (c *Config) IsP2P() bool {
	return c.Role == "" || c.Role == RoleP2P
}

// GetMasterConfig returns master config, initializing if needed
func (c *Config) GetMasterConfig() *MasterConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Master == nil {
		c.Master = DefaultMasterConfig()
	}
	return c.Master
}

// GetWorkerConfig returns worker config, initializing if needed
func (c *Config) GetWorkerConfig() *WorkerConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Worker == nil {
		c.Worker = DefaultWorkerConfig()
	}
	return c.Worker
}

// SetWorkerCredentials stores worker credentials after registration
func (c *Config) SetWorkerCredentials(workerID, authToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Worker == nil {
		c.Worker = DefaultWorkerConfig()
	}
	c.Worker.WorkerID = workerID
	c.Worker.AuthToken = authToken
}
