package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// TransferBytes tracks bytes transferred during migration
	TransferBytes = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "docker_migrate_transfer_bytes_total",
			Help: "Total bytes transferred during migrations",
		},
		[]string{"resource_type", "direction", "peer"},
	)

	// TransferDuration tracks transfer duration
	TransferDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "docker_migrate_transfer_duration_seconds",
			Help:    "Duration of resource transfers",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 15), // 0.1s to ~54 minutes
		},
		[]string{"resource_type", "status"},
	)

	// ActiveMigrations tracks currently running migrations
	ActiveMigrations = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "docker_migrate_active_migrations",
			Help: "Number of currently active migrations",
		},
	)

	// MigrationStatus tracks migration outcomes
	MigrationStatus = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "docker_migrate_migrations_total",
			Help: "Total number of migrations by status",
		},
		[]string{"status", "strategy"},
	)

	// ConnectedPeers tracks number of connected peers
	ConnectedPeers = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "docker_migrate_connected_peers",
			Help: "Number of currently connected peers",
		},
	)

	// DockerOperations tracks Docker SDK operation counts
	DockerOperations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "docker_migrate_docker_operations_total",
			Help: "Total number of Docker SDK operations",
		},
		[]string{"operation", "status"},
	)

	// DockerOperationDuration tracks Docker SDK operation latency
	DockerOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "docker_migrate_docker_operation_duration_seconds",
			Help:    "Duration of Docker SDK operations",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms to ~4 seconds
		},
		[]string{"operation"},
	)

	// VolumeSize tracks volume sizes
	VolumeSize = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "docker_migrate_volume_size_bytes",
			Help:    "Size of volumes being migrated",
			Buckets: prometheus.ExponentialBuckets(1024*1024, 2, 20), // 1MB to 1TB
		},
		[]string{"volume"},
	)

	// ChecksumVerifications tracks checksum verification results
	ChecksumVerifications = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "docker_migrate_checksum_verifications_total",
			Help: "Total number of checksum verifications",
		},
		[]string{"resource_type", "result"},
	)

	// RetryAttempts tracks retry attempts for failed operations
	RetryAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "docker_migrate_retry_attempts_total",
			Help: "Total number of retry attempts",
		},
		[]string{"operation", "outcome"},
	)

	// GRPCStreamErrors tracks gRPC streaming errors
	GRPCStreamErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "docker_migrate_grpc_stream_errors_total",
			Help: "Total number of gRPC streaming errors",
		},
		[]string{"method", "error_type"},
	)

	// BufferUtilization tracks buffer usage in streaming operations
	BufferUtilization = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "docker_migrate_buffer_utilization_ratio",
			Help:    "Buffer utilization ratio (0.0 to 1.0)",
			Buckets: prometheus.LinearBuckets(0, 0.1, 11), // 0% to 100%
		},
		[]string{"buffer_type"},
	)
)

// Metrics provides access to all application metrics
type Metrics struct{}

// NewMetrics creates a new Metrics instance
func NewMetrics() *Metrics {
	return &Metrics{}
}

// RecordTransfer records a transfer operation
func (m *Metrics) RecordTransfer(resourceType, direction, peer string, bytes float64) {
	TransferBytes.WithLabelValues(resourceType, direction, peer).Add(bytes)
}

// RecordMigration records a migration outcome
func (m *Metrics) RecordMigration(status, strategy string) {
	MigrationStatus.WithLabelValues(status, strategy).Inc()
}

// SetActiveMigrations sets the number of active migrations
func (m *Metrics) SetActiveMigrations(count float64) {
	ActiveMigrations.Set(count)
}

// SetConnectedPeers sets the number of connected peers
func (m *Metrics) SetConnectedPeers(count float64) {
	ConnectedPeers.Set(count)
}
