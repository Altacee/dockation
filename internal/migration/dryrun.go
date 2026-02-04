package migration

import (
	"time"
)

// DryRunResult contains comprehensive preview of migration operations
type DryRunResult struct {
	Operations         []Operation   `json:"operations"`
	TotalTransferBytes int64         `json:"total_transfer_bytes"`
	EstimatedDuration  time.Duration `json:"estimated_duration"`
	Warnings           []string      `json:"warnings"`
	Blockers           []string      `json:"blockers"`
}

// Operation represents a single migration operation
type Operation struct {
	Type         string   `json:"type"` // transfer_image, transfer_volume, create_network, create_container
	ResourceName string   `json:"resource_name"`
	ResourceID   string   `json:"resource_id"`
	SizeBytes    int64    `json:"size_bytes"`
	Conflicts    []string `json:"conflicts,omitempty"`
	Notes        []string `json:"notes,omitempty"`
}

// EstimateTransferTime calculates expected duration based on size and bandwidth
func EstimateTransferTime(bytes int64, bandwidthMbps int) time.Duration {
	if bandwidthMbps <= 0 {
		bandwidthMbps = 100 // Default to 100 Mbps
	}

	// Convert Mbps to bytes per second
	bytesPerSecond := (bandwidthMbps * 1024 * 1024) / 8

	// Calculate seconds needed
	seconds := bytes / int64(bytesPerSecond)

	// Add 20% overhead for compression, checksums, etc.
	seconds = int64(float64(seconds) * 1.2)

	return time.Duration(seconds) * time.Second
}
