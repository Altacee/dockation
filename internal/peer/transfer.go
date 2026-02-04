package peer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/config"
	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/cespare/xxhash/v2"
	"go.uber.org/zap"
)

const (
	// Chunk size limits
	MinChunkSize     = 256 * 1024  // 256KB minimum
	DefaultChunkSize = 1024 * 1024 // 1MB default
	MaxChunkSize     = 4 * 1024 * 1024 // 4MB maximum

	// Transfer configuration
	StableChunkCount    = 10
	CheckpointInterval  = 10 * time.Second
	KeepaliveInterval   = 30 * time.Second
	CheckpointBatchSize = 100 // Save checkpoint every N chunks
)

// TransferType identifies the type of resource being transferred
type TransferType int

const (
	TransferVolume TransferType = iota
	TransferImage
	TransferContainer
	TransferNetwork
)

func (t TransferType) String() string {
	switch t {
	case TransferVolume:
		return "volume"
	case TransferImage:
		return "image"
	case TransferContainer:
		return "container"
	case TransferNetwork:
		return "network"
	default:
		return "unknown"
	}
}

// TransferStatus represents the state of a transfer
type TransferStatus int

const (
	TransferPending TransferStatus = iota
	TransferActive
	TransferPaused
	TransferCompleted
	TransferFailed
)

func (s TransferStatus) String() string {
	switch s {
	case TransferPending:
		return "pending"
	case TransferActive:
		return "active"
	case TransferPaused:
		return "paused"
	case TransferCompleted:
		return "completed"
	case TransferFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// TransferManager manages all active transfers with checkpointing
type TransferManager struct {
	activeTransfers map[string]*Transfer
	config          *config.Config
	logger          *observability.Logger
	checkpointDir   string
	mu              sync.RWMutex
}

// Transfer represents an ongoing transfer operation
type Transfer struct {
	ID               string
	Type             TransferType
	SourceID         string
	DestPeer         string
	TotalBytes       int64
	TransferredBytes int64
	ChunkSize        int
	StartTime        time.Time
	LastChunkTime    time.Time
	LastCheckpoint   time.Time
	Checkpoints      []Checkpoint
	Status           TransferStatus
	Error            string
	Speed            float64 // bytes per second
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.RWMutex
}

// Checkpoint represents a recovery point
type Checkpoint struct {
	Offset    int64
	Checksum  string // xxhash64 for speed
	Timestamp time.Time
	Verified  bool
}

// Chunk represents a data chunk with checksum
type Chunk struct {
	Offset   int64
	Data     []byte
	Checksum string
	Size     int
	IsFinal  bool
}

// NewTransferManager creates a new transfer manager
func NewTransferManager(cfg *config.Config, logger *observability.Logger) (*TransferManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	checkpointDir := filepath.Join(homeDir, ".docker-migrate", "checkpoints")
	if err := os.MkdirAll(checkpointDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create checkpoint directory: %w", err)
	}

	return &TransferManager{
		activeTransfers: make(map[string]*Transfer),
		config:          cfg,
		logger:          logger,
		checkpointDir:   checkpointDir,
	}, nil
}

// CreateTransfer creates a new transfer
func (tm *TransferManager) CreateTransfer(ctx context.Context, transferType TransferType, sourceID, destPeer string, totalBytes int64) (*Transfer, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	transferID := generateTransferID(transferType, sourceID)

	// Check if transfer already exists - resume if pending/paused
	if existing, ok := tm.activeTransfers[transferID]; ok {
		if existing.Status == TransferActive {
			return nil, fmt.Errorf("transfer already active")
		}
		if existing.Status == TransferCompleted {
			return existing, nil
		}
		// Resume existing transfer
		return tm.resumeTransfer(ctx, existing)
	}

	ctx, cancel := context.WithCancel(ctx)

	transfer := &Transfer{
		ID:               transferID,
		Type:             transferType,
		SourceID:         sourceID,
		DestPeer:         destPeer,
		TotalBytes:       totalBytes,
		TransferredBytes: 0,
		ChunkSize:        DefaultChunkSize,
		StartTime:        time.Now(),
		LastChunkTime:    time.Now(),
		LastCheckpoint:   time.Now(),
		Checkpoints:      make([]Checkpoint, 0),
		Status:           TransferPending,
		ctx:              ctx,
		cancel:           cancel,
	}

	tm.activeTransfers[transferID] = transfer

	tm.logger.Info("created transfer",
		zap.String("transfer_id", transferID),
		zap.String("type", transferType.String()),
		zap.String("source_id", sourceID),
		zap.Int64("total_bytes", totalBytes),
	)

	return transfer, nil
}

// resumeTransfer resumes a paused transfer
func (tm *TransferManager) resumeTransfer(ctx context.Context, transfer *Transfer) (*Transfer, error) {
	// Load checkpoint if exists
	checkpointPath := filepath.Join(tm.checkpointDir, transfer.ID+".json")
	if _, err := os.Stat(checkpointPath); err == nil {
		if err := tm.loadCheckpoint(transfer); err != nil {
			tm.logger.Warn("failed to load checkpoint, starting fresh",
				zap.String("transfer_id", transfer.ID),
				zap.Error(err),
			)
			transfer.TransferredBytes = 0
			transfer.Checkpoints = make([]Checkpoint, 0)
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	transfer.ctx = ctx
	transfer.cancel = cancel
	transfer.Status = TransferPending
	transfer.LastChunkTime = time.Now()

	tm.logger.Info("resuming transfer",
		zap.String("transfer_id", transfer.ID),
		zap.Int64("resumed_from", transfer.TransferredBytes),
	)

	return transfer, nil
}

// ChunkReader wraps a reader with chunking and checksums
type ChunkReader struct {
	reader    io.Reader
	chunkSize int
	offset    int64
	totalSize int64
}

// NewChunkReader creates a new chunk reader
func NewChunkReader(reader io.Reader, chunkSize int, totalSize int64) *ChunkReader {
	if chunkSize < MinChunkSize {
		chunkSize = MinChunkSize
	}
	if chunkSize > MaxChunkSize {
		chunkSize = MaxChunkSize
	}

	return &ChunkReader{
		reader:    reader,
		chunkSize: chunkSize,
		offset:    0,
		totalSize: totalSize,
	}
}

// ReadChunk reads the next chunk with checksum
func (cr *ChunkReader) ReadChunk() (*Chunk, error) {
	buffer := make([]byte, cr.chunkSize)
	n, err := io.ReadFull(cr.reader, buffer)

	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("failed to read chunk: %w", err)
	}

	if n == 0 {
		return nil, io.EOF
	}

	// Trim buffer to actual read size
	data := buffer[:n]

	// Compute xxhash64 checksum (fast)
	hash := xxhash.Sum64(data)
	checksum := fmt.Sprintf("%016x", hash)

	chunk := &Chunk{
		Offset:   cr.offset,
		Data:     data,
		Checksum: checksum,
		Size:     n,
		IsFinal:  err == io.EOF || err == io.ErrUnexpectedEOF,
	}

	cr.offset += int64(n)

	return chunk, nil
}

// GetOffset returns current offset
func (cr *ChunkReader) GetOffset() int64 {
	return cr.offset
}

// ChunkWriter writes chunks with verification
type ChunkWriter struct {
	writer         io.Writer
	offset         int64
	expectedOffset int64
	logger         *observability.Logger
}

// NewChunkWriter creates a new chunk writer
func NewChunkWriter(writer io.Writer, startOffset int64, logger *observability.Logger) *ChunkWriter {
	return &ChunkWriter{
		writer:         writer,
		offset:         startOffset,
		expectedOffset: startOffset,
		logger:         logger,
	}
}

// WriteChunk writes and verifies a chunk
func (cw *ChunkWriter) WriteChunk(chunk *Chunk) error {
	// Verify offset continuity
	if chunk.Offset != cw.expectedOffset {
		return fmt.Errorf("chunk offset mismatch: expected %d, got %d", cw.expectedOffset, chunk.Offset)
	}

	// Verify checksum
	hash := xxhash.Sum64(chunk.Data)
	actualChecksum := fmt.Sprintf("%016x", hash)

	if actualChecksum != chunk.Checksum {
		return fmt.Errorf("chunk checksum mismatch at offset %d: expected %s, got %s",
			chunk.Offset, chunk.Checksum, actualChecksum)
	}

	// Write data
	n, err := cw.writer.Write(chunk.Data)
	if err != nil {
		return fmt.Errorf("failed to write chunk at offset %d: %w", chunk.Offset, err)
	}

	if n != len(chunk.Data) {
		return fmt.Errorf("incomplete write at offset %d: wrote %d of %d bytes", chunk.Offset, n, len(chunk.Data))
	}

	cw.offset += int64(n)
	cw.expectedOffset += int64(n)

	return nil
}

// GetOffset returns current offset
func (cw *ChunkWriter) GetOffset() int64 {
	return cw.offset
}

// DynamicChunkSize adjusts chunk size based on transfer performance
func (tm *TransferManager) DynamicChunkSize(transfer *Transfer) int {
	transfer.mu.RLock()
	defer transfer.mu.RUnlock()

	// Start at default
	if len(transfer.Checkpoints) < StableChunkCount {
		return DefaultChunkSize
	}

	// Calculate recent performance
	recentCheckpoints := transfer.Checkpoints
	if len(recentCheckpoints) > StableChunkCount {
		recentCheckpoints = recentCheckpoints[len(recentCheckpoints)-StableChunkCount:]
	}

	// Check if transfers have been stable (no errors in last N chunks)
	stable := true
	for i := 1; i < len(recentCheckpoints); i++ {
		timeDiff := recentCheckpoints[i].Timestamp.Sub(recentCheckpoints[i-1].Timestamp)
		if timeDiff > 2*time.Second {
			stable = false
			break
		}
	}

	currentSize := transfer.ChunkSize

	if stable && currentSize < MaxChunkSize {
		// Double size up to maximum
		newSize := currentSize * 2
		if newSize > MaxChunkSize {
			newSize = MaxChunkSize
		}
		return newSize
	}

	if !stable && currentSize > MinChunkSize {
		// Halve size down to minimum
		newSize := currentSize / 2
		if newSize < MinChunkSize {
			newSize = MinChunkSize
		}
		return newSize
	}

	return currentSize
}

// AddCheckpoint adds a checkpoint for the transfer
func (tm *TransferManager) AddCheckpoint(transferID string, offset int64, checksum string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	transfer, ok := tm.activeTransfers[transferID]
	if !ok {
		return fmt.Errorf("transfer not found: %s", transferID)
	}

	transfer.mu.Lock()
	defer transfer.mu.Unlock()

	checkpoint := Checkpoint{
		Offset:    offset,
		Checksum:  checksum,
		Timestamp: time.Now(),
		Verified:  true,
	}

	transfer.Checkpoints = append(transfer.Checkpoints, checkpoint)
	transfer.TransferredBytes = offset
	transfer.LastChunkTime = time.Now()

	// Calculate speed
	elapsed := time.Since(transfer.StartTime).Seconds()
	if elapsed > 0 {
		transfer.Speed = float64(transfer.TransferredBytes) / elapsed
	}

	// Save checkpoint periodically
	if len(transfer.Checkpoints)%CheckpointBatchSize == 0 {
		if err := tm.saveCheckpoint(transfer); err != nil {
			tm.logger.Warn("failed to save checkpoint",
				zap.String("transfer_id", transferID),
				zap.Error(err),
			)
		}
	}

	return nil
}

// saveCheckpoint saves transfer state to disk
func (tm *TransferManager) saveCheckpoint(transfer *Transfer) error {
	checkpointPath := filepath.Join(tm.checkpointDir, transfer.ID+".json")
	tmpPath := checkpointPath + ".tmp"

	data, err := json.MarshalIndent(transfer, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	// Atomic write
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write checkpoint: %w", err)
	}

	if err := os.Rename(tmpPath, checkpointPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename checkpoint: %w", err)
	}

	transfer.LastCheckpoint = time.Now()

	tm.logger.Debug("saved checkpoint",
		zap.String("transfer_id", transfer.ID),
		zap.Int64("offset", transfer.TransferredBytes),
	)

	return nil
}

// loadCheckpoint loads transfer state from disk
func (tm *TransferManager) loadCheckpoint(transfer *Transfer) error {
	checkpointPath := filepath.Join(tm.checkpointDir, transfer.ID+".json")

	data, err := os.ReadFile(checkpointPath)
	if err != nil {
		return fmt.Errorf("failed to read checkpoint: %w", err)
	}

	if err := json.Unmarshal(data, transfer); err != nil {
		return fmt.Errorf("failed to unmarshal checkpoint: %w", err)
	}

	tm.logger.Info("loaded checkpoint",
		zap.String("transfer_id", transfer.ID),
		zap.Int64("offset", transfer.TransferredBytes),
		zap.Int("checkpoints", len(transfer.Checkpoints)),
	)

	return nil
}

// GetTransfer retrieves a transfer by ID
func (tm *TransferManager) GetTransfer(transferID string) (*Transfer, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	transfer, ok := tm.activeTransfers[transferID]
	return transfer, ok
}

// CompleteTransfer marks a transfer as completed
func (tm *TransferManager) CompleteTransfer(transferID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	transfer, ok := tm.activeTransfers[transferID]
	if !ok {
		return fmt.Errorf("transfer not found: %s", transferID)
	}

	transfer.mu.Lock()
	defer transfer.mu.Unlock()

	transfer.Status = TransferCompleted

	// Save final checkpoint
	if err := tm.saveCheckpoint(transfer); err != nil {
		tm.logger.Warn("failed to save final checkpoint", zap.Error(err))
	}

	tm.logger.Info("transfer completed",
		zap.String("transfer_id", transferID),
		zap.Int64("total_bytes", transfer.TotalBytes),
		zap.Duration("duration", time.Since(transfer.StartTime)),
		zap.Float64("avg_speed_mbps", transfer.Speed/(1024*1024)),
	)

	// Clean up checkpoint file after successful completion
	checkpointPath := filepath.Join(tm.checkpointDir, transferID+".json")
	os.Remove(checkpointPath)

	return nil
}

// FailTransfer marks a transfer as failed
func (tm *TransferManager) FailTransfer(transferID string, err error) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	transfer, ok := tm.activeTransfers[transferID]
	if !ok {
		return fmt.Errorf("transfer not found: %s", transferID)
	}

	transfer.mu.Lock()
	defer transfer.mu.Unlock()

	transfer.Status = TransferFailed
	transfer.Error = err.Error()

	// Save checkpoint for recovery
	if saveErr := tm.saveCheckpoint(transfer); saveErr != nil {
		tm.logger.Warn("failed to save checkpoint on failure", zap.Error(saveErr))
	}

	tm.logger.Error("transfer failed",
		zap.String("transfer_id", transferID),
		zap.Int64("bytes_transferred", transfer.TransferredBytes),
		zap.Error(err),
	)

	return nil
}

// CancelTransfer cancels an active transfer
func (tm *TransferManager) CancelTransfer(transferID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	transfer, ok := tm.activeTransfers[transferID]
	if !ok {
		return fmt.Errorf("transfer not found: %s", transferID)
	}

	transfer.cancel()

	transfer.mu.Lock()
	transfer.Status = TransferPaused
	transfer.mu.Unlock()

	// Save checkpoint for resume
	if err := tm.saveCheckpoint(transfer); err != nil {
		tm.logger.Warn("failed to save checkpoint on cancel", zap.Error(err))
	}

	tm.logger.Info("transfer cancelled",
		zap.String("transfer_id", transferID),
	)

	return nil
}

// ListActiveTransfers returns all active transfers
func (tm *TransferManager) ListActiveTransfers() []*Transfer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	transfers := make([]*Transfer, 0, len(tm.activeTransfers))
	for _, transfer := range tm.activeTransfers {
		transfers = append(transfers, transfer)
	}

	return transfers
}

// ComputeFileChecksum computes SHA-256 checksum of entire file
func ComputeFileChecksum(reader io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", fmt.Errorf("failed to compute checksum: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// generateTransferID generates a unique transfer ID
func generateTransferID(transferType TransferType, sourceID string) string {
	combined := fmt.Sprintf("%s-%s-%d", transferType.String(), sourceID, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(combined))
	return fmt.Sprintf("transfer-%x", hash[:8])
}
