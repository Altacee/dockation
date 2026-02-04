package docker

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/docker/docker/api/types/volume"
	"go.uber.org/zap"
)

// VolumeInfo represents detailed volume information
type VolumeInfo struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Labels     map[string]string `json:"labels"`
	Options    map[string]string `json:"options"`
	Scope      string            `json:"scope"`
	Size       int64             `json:"size"`
}

// ListVolumes returns all volumes
func (c *Client) ListVolumes(ctx context.Context) ([]*volume.Volume, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	resp, err := cli.VolumeList(ctx, volume.ListOptions{})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("volume_list").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("volume_list", "error").Inc()
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	observability.DockerOperations.WithLabelValues("volume_list", "success").Inc()
	c.logger.Info("listed volumes", zap.Int("count", len(resp.Volumes)))

	return resp.Volumes, nil
}

// InspectVolume retrieves detailed volume information
func (c *Client) InspectVolume(ctx context.Context, volumeName string) (*volume.Volume, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	vol, err := cli.VolumeInspect(ctx, volumeName)
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("volume_inspect").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("volume_inspect", "error").Inc()
		return nil, fmt.Errorf("failed to inspect volume %s: %w", volumeName, err)
	}

	observability.DockerOperations.WithLabelValues("volume_inspect", "success").Inc()
	return &vol, nil
}

// GetVolumeInfo retrieves comprehensive volume information including size
func (c *Client) GetVolumeInfo(ctx context.Context, volumeName string) (*VolumeInfo, error) {
	c.logger.Info("getting volume info", zap.String("volume", volumeName))

	vol, err := c.InspectVolume(ctx, volumeName)
	if err != nil {
		return nil, err
	}

	// Calculate volume size
	size, err := c.calculateVolumeSize(ctx, vol.Mountpoint)
	if err != nil {
		c.logger.Warn("failed to calculate volume size",
			zap.String("volume", volumeName),
			zap.Error(err),
		)
		size = 0
	}

	info := &VolumeInfo{
		Name:       vol.Name,
		Driver:     vol.Driver,
		Mountpoint: vol.Mountpoint,
		Labels:     vol.Labels,
		Options:    vol.Options,
		Scope:      vol.Scope,
		Size:       size,
	}

	observability.VolumeSize.WithLabelValues(volumeName).Observe(float64(size))

	c.logger.Info("volume info retrieved",
		zap.String("volume", volumeName),
		zap.Int64("size", size),
		zap.String("driver", vol.Driver),
	)

	return info, nil
}

// GetVolumeSize calculates the actual size of a volume's data
func (c *Client) GetVolumeSize(ctx context.Context, volumeName string) (int64, error) {
	vol, err := c.InspectVolume(ctx, volumeName)
	if err != nil {
		return 0, err
	}

	return c.calculateVolumeSize(ctx, vol.Mountpoint)
}

// calculateVolumeSize walks the volume directory to calculate total size
func (c *Client) calculateVolumeSize(ctx context.Context, mountpoint string) (int64, error) {
	var totalSize int64

	err := filepath.Walk(mountpoint, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Log but don't fail on permission errors
			if os.IsPermission(err) {
				c.logger.Warn("permission denied while calculating size",
					zap.String("path", path),
				)
				return nil
			}
			return err
		}

		// Check for context cancellation periodically
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !info.IsDir() {
			totalSize += info.Size()
		}

		return nil
	})

	if err != nil && err != context.Canceled {
		return 0, fmt.Errorf("failed to calculate volume size: %w", err)
	}

	return totalSize, nil
}

// ExportVolume exports a volume as a tar stream
// The returned reader must be closed by the caller
func (c *Client) ExportVolume(ctx context.Context, volumeName string) (io.ReadCloser, error) {
	c.logger.Info("exporting volume", zap.String("volume", volumeName))

	// Verify volume exists
	vol, err := c.InspectVolume(ctx, volumeName)
	if err != nil {
		return nil, fmt.Errorf("volume verification failed: %w", err)
	}

	// Create pipe for streaming
	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()

		if err := c.createVolumeTar(ctx, vol.Mountpoint, pw); err != nil {
			c.logger.Error("failed to create volume tar",
				zap.String("volume", volumeName),
				zap.Error(err),
			)
			pw.CloseWithError(err)
			return
		}

		c.logger.Info("volume export completed", zap.String("volume", volumeName))
	}()

	return &volumeReader{
		ReadCloser: pr,
		volumeName: volumeName,
		logger:     c.logger,
		startTime:  time.Now(),
	}, nil
}

// createVolumeTar creates a tar archive of the volume contents
func (c *Client) createVolumeTar(ctx context.Context, mountpoint string, w io.Writer) error {
	tw := tar.NewWriter(w)
	defer tw.Close()

	baseDir := mountpoint
	return filepath.Walk(mountpoint, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsPermission(err) {
				c.logger.Warn("permission denied while archiving",
					zap.String("path", path),
				)
				return nil
			}
			return err
		}

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("failed to create tar header: %w", err)
		}

		// Preserve relative path
		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header: %w", err)
		}

		// Write file contents if not a directory
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				if os.IsPermission(err) {
					c.logger.Warn("cannot read file",
						zap.String("path", path),
					)
					return nil
				}
				return fmt.Errorf("failed to open file: %w", err)
			}
			defer file.Close()

			if _, err := io.Copy(tw, file); err != nil {
				return fmt.Errorf("failed to write file contents: %w", err)
			}
		}

		return nil
	})
}

// ImportVolume imports a volume from a tar stream
func (c *Client) ImportVolume(ctx context.Context, volumeName string, reader io.Reader) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	c.mu.RUnlock()

	c.logger.Info("importing volume", zap.String("volume", volumeName))

	// Create volume if it doesn't exist
	vol, err := c.InspectVolume(ctx, volumeName)
	if err != nil {
		// Volume doesn't exist, create it
		vol, err = c.CreateVolume(ctx, volumeName, nil, nil)
		if err != nil {
			return fmt.Errorf("failed to create volume: %w", err)
		}
	}

	// Extract tar to volume mountpoint
	if err := c.extractVolumeTar(ctx, vol.Mountpoint, reader); err != nil {
		return fmt.Errorf("failed to extract volume tar: %w", err)
	}

	c.logger.Info("volume imported successfully", zap.String("volume", volumeName))
	return nil
}

// extractVolumeTar extracts a tar archive to the volume mountpoint
func (c *Client) extractVolumeTar(ctx context.Context, mountpoint string, r io.Reader) error {
	tr := tar.NewReader(r)

	for {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		target := filepath.Join(mountpoint, header.Name)

		// Security check: prevent path traversal
		if !filepath.HasPrefix(target, filepath.Clean(mountpoint)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid tar path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Write to temporary file first, then atomic rename
			tmpFile := target + ".tmp"
			file, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(file, tr); err != nil {
				file.Close()
				os.Remove(tmpFile)
				return fmt.Errorf("failed to write file: %w", err)
			}

			if err := file.Sync(); err != nil {
				file.Close()
				os.Remove(tmpFile)
				return fmt.Errorf("failed to sync file: %w", err)
			}

			file.Close()

			// Atomic rename
			if err := os.Rename(tmpFile, target); err != nil {
				os.Remove(tmpFile)
				return fmt.Errorf("failed to rename file: %w", err)
			}
		}
	}

	return nil
}

// CreateVolume creates a new volume
func (c *Client) CreateVolume(ctx context.Context, name string, labels, options map[string]string) (*volume.Volume, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	vol, err := cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:   name,
		Labels: labels,
		Driver: "local",
		DriverOpts: options,
	})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("volume_create").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("volume_create", "error").Inc()
		return nil, fmt.Errorf("failed to create volume %s: %w", name, err)
	}

	observability.DockerOperations.WithLabelValues("volume_create", "success").Inc()
	c.logger.Info("volume created", zap.String("volume", name))

	return &vol, nil
}

// RemoveVolume removes a volume
func (c *Client) RemoveVolume(ctx context.Context, volumeName string, force bool) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	err := cli.VolumeRemove(ctx, volumeName, force)
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("volume_remove").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("volume_remove", "error").Inc()
		return fmt.Errorf("failed to remove volume %s: %w", volumeName, err)
	}

	observability.DockerOperations.WithLabelValues("volume_remove", "success").Inc()
	c.logger.Info("volume removed", zap.String("volume", volumeName))
	return nil
}

// volumeReader wraps an io.ReadCloser to track volume transfer metrics
type volumeReader struct {
	io.ReadCloser
	volumeName string
	logger     *observability.Logger
	bytesRead  int64
	startTime  time.Time
	closeOnce  bool
}

func (vr *volumeReader) Read(p []byte) (n int, err error) {
	n, err = vr.ReadCloser.Read(p)
	vr.bytesRead += int64(n)

	if n > 0 {
		observability.TransferBytes.WithLabelValues("volume", "export", "local").Add(float64(n))
	}

	return n, err
}

func (vr *volumeReader) Close() error {
	if !vr.closeOnce {
		vr.closeOnce = true
		duration := time.Since(vr.startTime)
		vr.logger.Info("volume export completed",
			zap.String("volume", vr.volumeName),
			zap.Int64("bytes", vr.bytesRead),
			zap.Duration("duration", duration),
		)
	}
	return vr.ReadCloser.Close()
}
