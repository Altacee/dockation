package docker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

// Client wraps the Docker SDK client with enhanced error handling and observability
type Client struct {
	cli    *client.Client
	logger *observability.Logger
	mu     sync.RWMutex
	closed bool
}

// NewClient creates a new Docker client with connection validation
func NewClient(logger *observability.Logger, host string) (*Client, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}

	if host != "" {
		opts = append(opts, client.WithHost(host))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	dc := &Client{
		cli:    cli,
		logger: logger,
	}

	// Verify connection immediately
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := dc.Ping(ctx); err != nil {
		cli.Close()
		return nil, fmt.Errorf("docker daemon unreachable: %w", err)
	}

	logger.Info("docker client connected successfully")
	return dc, nil
}

// Ping verifies the Docker daemon is reachable
func (c *Client) Ping(ctx context.Context) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	cli := c.cli
	c.mu.RUnlock()

	start := time.Now()
	_, err := cli.Ping(ctx)
	duration := time.Since(start)

	observability.DockerOperationDuration.WithLabelValues("ping").Observe(duration.Seconds())

	if err != nil {
		observability.DockerOperations.WithLabelValues("ping", "error").Inc()
		return fmt.Errorf("ping failed: %w", err)
	}

	observability.DockerOperations.WithLabelValues("ping", "success").Inc()
	return nil
}

// Close closes the Docker client connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	if err := c.cli.Close(); err != nil {
		c.logger.Error("failed to close docker client", zap.Error(err))
		return err
	}

	c.logger.Info("docker client closed")
	return nil
}

// Raw returns the underlying Docker SDK client
// WARNING: Direct use bypasses observability and error handling
func (c *Client) Raw() *client.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cli
}

// IsClosed returns true if the client has been closed
func (c *Client) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// withRetry executes an operation with exponential backoff retry logic
func (c *Client) withRetry(ctx context.Context, operation string, maxRetries int, fn func() error) error {
	backoff := time.Second
	maxBackoff := time.Minute

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				observability.RetryAttempts.WithLabelValues(operation, "cancelled").Inc()
				return fmt.Errorf("operation cancelled during retry: %w", ctx.Err())
			case <-time.After(backoff):
				// Add jitter to prevent thundering herd
				backoff = backoff * 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}

			c.logger.Info("retrying operation",
				zap.String("operation", operation),
				zap.Int("attempt", attempt),
				zap.Duration("backoff", backoff),
			)
		}

		if err := fn(); err != nil {
			lastErr = err
			if !isRetriableError(err) {
				observability.RetryAttempts.WithLabelValues(operation, "permanent_failure").Inc()
				return err
			}
			observability.RetryAttempts.WithLabelValues(operation, "retry").Inc()
			continue
		}

		if attempt > 0 {
			observability.RetryAttempts.WithLabelValues(operation, "success_after_retry").Inc()
		}
		return nil
	}

	observability.RetryAttempts.WithLabelValues(operation, "exhausted").Inc()
	return fmt.Errorf("operation %s failed after %d retries: %w", operation, maxRetries, lastErr)
}

// isRetriableError determines if an error is transient and can be retried
func isRetriableError(err error) bool {
	if err == nil {
		return false
	}

	// Common retriable errors from Docker SDK
	errStr := err.Error()
	retriablePatterns := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"temporary failure",
		"TLS handshake timeout",
		"EOF",
		"broken pipe",
	}

	for _, pattern := range retriablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}

	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsAny(s, substr))
}

func containsAny(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
