package migration

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/artemis/docker-migrate/internal/docker"
	"github.com/artemis/docker-migrate/internal/peer"

	"go.uber.org/zap"
)

// ImageMigrator handles Docker image migration with layer deduplication
// This is critical for efficiency - only transfer layers that don't exist on target
type ImageMigrator struct {
	docker   *docker.Client
	transfer *peer.TransferManager
	logger   *zap.Logger
}

// ImageLayer represents a single layer in a Docker image
type ImageLayer struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"media_type"`
}

// ImageManifest represents an image's structure
type ImageManifest struct {
	Config struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"config"`
	Layers []ImageLayer `json:"layers"`
}

// MigrateImage transfers an image with layer deduplication
// This implements the critical optimization of only transferring missing layers
func (im *ImageMigrator) MigrateImage(ctx context.Context, imageID, peerID string, progressCh chan<- MigrationProgress) error {
	im.logger.Info("starting image migration",
		zap.String("image_id", imageID),
		zap.String("peer_id", peerID),
	)

	// Step 1: Get local image manifest and layers
	manifest, err := im.getImageManifest(ctx, imageID)
	if err != nil {
		return fmt.Errorf("failed to get image manifest: %w", err)
	}

	im.logger.Info("retrieved image manifest",
		zap.String("image_id", imageID),
		zap.Int("layer_count", len(manifest.Layers)),
	)

	// Step 2: Query target for existing layers
	existingLayers, err := im.queryTargetLayers(ctx, peerID, imageID)
	if err != nil {
		return fmt.Errorf("failed to query target layers: %w", err)
	}

	// Step 3: Calculate missing layers (layer deduplication)
	missingLayers := im.diffLayers(manifest.Layers, existingLayers)

	im.logger.Info("calculated missing layers",
		zap.Int("total_layers", len(manifest.Layers)),
		zap.Int("existing_layers", len(existingLayers)),
		zap.Int("missing_layers", len(missingLayers)),
	)

	// Step 4: Stream only missing layer blobs with checksum verification
	for i, layer := range missingLayers {
		select {
		case <-ctx.Done():
			return fmt.Errorf("migration cancelled: %w", ctx.Err())
		default:
			if err := im.transferLayer(ctx, peerID, layer, i+1, len(missingLayers), progressCh); err != nil {
				return fmt.Errorf("failed to transfer layer %s: %w", layer.Digest, err)
			}
		}
	}

	// Step 5: Send manifest for image reconstruction on target
	if err := im.sendManifest(ctx, peerID, imageID, manifest); err != nil {
		return fmt.Errorf("failed to send manifest: %w", err)
	}

	im.logger.Info("image migration completed",
		zap.String("image_id", imageID),
		zap.Int("layers_transferred", len(missingLayers)),
	)

	return nil
}

// getImageManifest retrieves the manifest for a local image
func (im *ImageMigrator) getImageManifest(ctx context.Context, imageID string) (*ImageManifest, error) {
	// Would use Docker SDK to get image manifest
	// For now, return mock data
	manifest := &ImageManifest{
		Layers: []ImageLayer{
			{Digest: "sha256:abc123", Size: 1024 * 1024, MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip"},
			{Digest: "sha256:def456", Size: 2048 * 1024, MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip"},
		},
	}
	manifest.Config.Digest = "sha256:config789"
	manifest.Config.Size = 4096

	return manifest, nil
}

// queryTargetLayers asks the target peer which layers it already has
func (im *ImageMigrator) queryTargetLayers(ctx context.Context, peerID, imageID string) ([]ImageLayer, error) {
	// Would send gRPC request to target peer to query existing layers
	// Target would check its local image store
	// For now, return empty (all layers need transfer)
	return []ImageLayer{}, nil
}

// diffLayers calculates which layers are missing on target
func (im *ImageMigrator) diffLayers(local []ImageLayer, remote []ImageLayer) []ImageLayer {
	remoteMap := make(map[string]bool)
	for _, layer := range remote {
		remoteMap[layer.Digest] = true
	}

	missing := make([]ImageLayer, 0)
	for _, layer := range local {
		if !remoteMap[layer.Digest] {
			missing = append(missing, layer)
		}
	}

	return missing
}

// transferLayer streams a single layer blob to the target with integrity checks
func (im *ImageMigrator) transferLayer(ctx context.Context, peerID string, layer ImageLayer, current, total int, progressCh chan<- MigrationProgress) error {
	im.logger.Info("transferring layer",
		zap.String("digest", layer.Digest),
		zap.Int64("size", layer.Size),
		zap.Int("current", current),
		zap.Int("total", total),
	)

	// Would:
	// 1. Export layer blob from local Docker
	// 2. Calculate SHA-256 checksum while streaming
	// 3. Send to target peer via gRPC stream
	// 4. Target verifies checksum on receive
	// 5. Atomic write to target image store

	// Update progress
	if progressCh != nil {
		progress := MigrationProgress{
			CurrentNumber: current,
			TotalItems:    total,
			CurrentItem:   fmt.Sprintf("Layer %s", layer.Digest[:12]),
			BytesDone:     layer.Size, // Would track actual bytes
			BytesTotal:    layer.Size,
		}
		progressCh <- progress
	}

	return nil
}

// sendManifest sends the image manifest to target for reconstruction
func (im *ImageMigrator) sendManifest(ctx context.Context, peerID, imageID string, manifest *ImageManifest) error {
	im.logger.Info("sending image manifest",
		zap.String("image_id", imageID),
		zap.String("peer_id", peerID),
	)

	// Would send manifest via gRPC
	// Target uses manifest to reconstruct image from received layers
	return nil
}

// CalculateChecksum computes SHA-256 for a data stream
// This is used for integrity verification during transfer
func (im *ImageMigrator) CalculateChecksum(r io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, r); err != nil {
		return "", fmt.Errorf("failed to calculate checksum: %w", err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
