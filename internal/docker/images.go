package docker

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"
	"go.uber.org/zap"
)

// ImageInfo represents detailed image information including layers
type ImageInfo struct {
	ID          string            `json:"id"`
	RepoTags    []string          `json:"repo_tags"`
	RepoDigests []string          `json:"repo_digests"`
	Size        int64             `json:"size"`
	Created     time.Time         `json:"created"`
	Labels      map[string]string `json:"labels"`
	Layers      []string          `json:"layers"` // Layer hashes
}

// ListImages returns all images with detailed information
func (c *Client) ListImages(ctx context.Context) ([]image.Summary, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	images, err := cli.ImageList(ctx, types.ImageListOptions{All: true})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("image_list").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("image_list", "error").Inc()
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	observability.DockerOperations.WithLabelValues("image_list", "success").Inc()
	c.logger.Info("listed images", zap.Int("count", len(images)))

	return images, nil
}

// InspectImage retrieves detailed image information
func (c *Client) InspectImage(ctx context.Context, imageID string) (types.ImageInspect, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return types.ImageInspect{}, fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	inspect, _, err := cli.ImageInspectWithRaw(ctx, imageID)
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("image_inspect").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("image_inspect", "error").Inc()
		return types.ImageInspect{}, fmt.Errorf("failed to inspect image %s: %w", imageID, err)
	}

	observability.DockerOperations.WithLabelValues("image_inspect", "success").Inc()
	return inspect, nil
}

// GetImageInfo retrieves comprehensive image information including layers
func (c *Client) GetImageInfo(ctx context.Context, imageID string) (*ImageInfo, error) {
	c.logger.Info("getting image info", zap.String("image_id", imageID))

	inspect, err := c.InspectImage(ctx, imageID)
	if err != nil {
		return nil, err
	}

	// Parse created time
	created, _ := time.Parse(time.RFC3339Nano, inspect.Created)

	info := &ImageInfo{
		ID:          inspect.ID,
		RepoTags:    inspect.RepoTags,
		RepoDigests: inspect.RepoDigests,
		Size:        inspect.Size,
		Created:     created,
		Labels:      inspect.Config.Labels,
		Layers:      inspect.RootFS.Layers,
	}

	c.logger.Info("image info retrieved",
		zap.String("image_id", imageID),
		zap.Int64("size", info.Size),
		zap.Int("layers", len(info.Layers)),
		zap.Strings("tags", info.RepoTags),
	)

	return info, nil
}

// GetImageLayers returns the layer hashes for an image
func (c *Client) GetImageLayers(ctx context.Context, imageID string) ([]string, error) {
	info, err := c.GetImageInfo(ctx, imageID)
	if err != nil {
		return nil, err
	}

	if len(info.Layers) == 0 {
		c.logger.Warn("image has no layers", zap.String("image_id", imageID))
	}

	return info.Layers, nil
}

// ExportImage exports an image as a tar stream
// The returned reader must be closed by the caller
func (c *Client) ExportImage(ctx context.Context, imageID string) (io.ReadCloser, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	c.logger.Info("exporting image", zap.String("image_id", imageID))

	// Verify image exists before attempting export
	if _, err := c.InspectImage(ctx, imageID); err != nil {
		return nil, fmt.Errorf("image verification failed: %w", err)
	}

	start := time.Now()
	reader, err := cli.ImageSave(ctx, []string{imageID})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("image_save").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("image_save", "error").Inc()
		return nil, fmt.Errorf("failed to export image %s: %w", imageID, err)
	}

	observability.DockerOperations.WithLabelValues("image_save", "success").Inc()
	c.logger.Info("image export started", zap.String("image_id", imageID))

	// Wrap reader to track metrics on close
	return &metricReader{
		ReadCloser: reader,
		imageID:    imageID,
		logger:     c.logger,
	}, nil
}

// ImportImage imports an image from a tar stream
func (c *Client) ImportImage(ctx context.Context, reader io.Reader) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	c.logger.Info("importing image")

	start := time.Now()
	resp, err := cli.ImageLoad(ctx, reader, true)
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("image_load").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("image_load", "error").Inc()
		return fmt.Errorf("failed to import image: %w", err)
	}
	defer resp.Body.Close()

	observability.DockerOperations.WithLabelValues("image_load", "success").Inc()

	// Read response to ensure load completed successfully
	output, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Warn("failed to read image load response", zap.Error(err))
	} else {
		c.logger.Info("image import response", zap.ByteString("output", output))
	}

	c.logger.Info("image imported successfully")
	return nil
}

// PullImage pulls an image from a registry
func (c *Client) PullImage(ctx context.Context, refStr string) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	c.logger.Info("pulling image", zap.String("ref", refStr))

	start := time.Now()
	reader, err := cli.ImagePull(ctx, refStr, types.ImagePullOptions{})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("image_pull").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("image_pull", "error").Inc()
		return fmt.Errorf("failed to pull image %s: %w", refStr, err)
	}
	defer reader.Close()

	observability.DockerOperations.WithLabelValues("image_pull", "success").Inc()

	// Read pull output to ensure completion
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("failed to read pull output: %w", err)
	}

	c.logger.Info("image pulled successfully", zap.String("ref", refStr))
	return nil
}

// RemoveImage removes an image
func (c *Client) RemoveImage(ctx context.Context, imageID string, force bool) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	_, err := cli.ImageRemove(ctx, imageID, types.ImageRemoveOptions{
		Force:         force,
		PruneChildren: true,
	})
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("image_remove").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("image_remove", "error").Inc()
		return fmt.Errorf("failed to remove image %s: %w", imageID, err)
	}

	observability.DockerOperations.WithLabelValues("image_remove", "success").Inc()
	c.logger.Info("image removed", zap.String("image_id", imageID))
	return nil
}

// metricReader wraps an io.ReadCloser to track metrics
type metricReader struct {
	io.ReadCloser
	imageID    string
	logger     *observability.Logger
	bytesRead  int64
	startTime  time.Time
	closeOnce  bool
}

func (mr *metricReader) Read(p []byte) (n int, err error) {
	if mr.startTime.IsZero() {
		mr.startTime = time.Now()
	}

	n, err = mr.ReadCloser.Read(p)
	mr.bytesRead += int64(n)

	if n > 0 {
		observability.TransferBytes.WithLabelValues("image", "export", "local").Add(float64(n))
	}

	return n, err
}

func (mr *metricReader) Close() error {
	if !mr.closeOnce {
		mr.closeOnce = true
		duration := time.Since(mr.startTime)
		mr.logger.Info("image export completed",
			zap.String("image_id", mr.imageID),
			zap.Int64("bytes", mr.bytesRead),
			zap.Duration("duration", duration),
		)
	}
	return mr.ReadCloser.Close()
}
