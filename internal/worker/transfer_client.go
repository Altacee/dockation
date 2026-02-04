package worker

import (
	"context"
	"fmt"
	"io"
	"sync"

	pb "github.com/artemis/docker-migrate/proto"
	"google.golang.org/grpc"
)

// TransferClient abstracts direct vs proxy transfer
type TransferClient interface {
	TransferVolume(ctx context.Context) (VolumeStream, error)
	TransferImageLayers(ctx context.Context) (ImageStream, error)
	Close() error
}

// VolumeStream abstracts the volume transfer stream
type VolumeStream interface {
	Send(*pb.VolumeChunk) error
	Recv() (*pb.TransferAck, error)
	CloseSend() error
}

// ImageStream abstracts the image layer transfer stream
type ImageStream interface {
	Send(*pb.LayerBlob) error
	Recv() (*pb.TransferAck, error)
	CloseSend() error
}

// DirectTransferClient wraps pb.MigrationServiceClient for direct mode
type DirectTransferClient struct {
	client pb.MigrationServiceClient
	conn   *grpc.ClientConn
}

// NewDirectTransferClient creates a new DirectTransferClient
func NewDirectTransferClient(client pb.MigrationServiceClient, conn *grpc.ClientConn) *DirectTransferClient {
	return &DirectTransferClient{
		client: client,
		conn:   conn,
	}
}

// TransferVolume opens a volume transfer stream in direct mode
func (d *DirectTransferClient) TransferVolume(ctx context.Context) (VolumeStream, error) {
	stream, err := d.client.TransferVolume(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open volume transfer stream: %w", err)
	}
	return &directVolumeStream{stream: stream}, nil
}

// TransferImageLayers opens an image layer transfer stream in direct mode
func (d *DirectTransferClient) TransferImageLayers(ctx context.Context) (ImageStream, error) {
	stream, err := d.client.TransferImageLayers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open image transfer stream: %w", err)
	}
	return &directImageStream{stream: stream}, nil
}

// Close closes the underlying connection
func (d *DirectTransferClient) Close() error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

// directVolumeStream wraps the direct gRPC stream for volumes
type directVolumeStream struct {
	stream pb.MigrationService_TransferVolumeClient
}

func (s *directVolumeStream) Send(chunk *pb.VolumeChunk) error {
	return s.stream.Send(chunk)
}

func (s *directVolumeStream) Recv() (*pb.TransferAck, error) {
	return s.stream.Recv()
}

func (s *directVolumeStream) CloseSend() error {
	return s.stream.CloseSend()
}

// directImageStream wraps the direct gRPC stream for images
type directImageStream struct {
	stream pb.MigrationService_TransferImageLayersClient
}

func (s *directImageStream) Send(blob *pb.LayerBlob) error {
	return s.stream.Send(blob)
}

func (s *directImageStream) Recv() (*pb.TransferAck, error) {
	return s.stream.Recv()
}

func (s *directImageStream) CloseSend() error {
	return s.stream.CloseSend()
}

// ProxyTransferClient wraps the proxy stream for proxy mode
type ProxyTransferClient struct {
	stream      pb.ProxyService_OpenProxyChannelClient
	migrationID string
	conn        *grpc.ClientConn
	workerID    string

	mu     sync.Mutex
	closed bool
}

// NewProxyTransferClient creates a new ProxyTransferClient
func NewProxyTransferClient(stream pb.ProxyService_OpenProxyChannelClient, migrationID string, conn *grpc.ClientConn) *ProxyTransferClient {
	return &ProxyTransferClient{
		stream:      stream,
		migrationID: migrationID,
		conn:        conn,
	}
}

// SetWorkerID sets the worker ID for proxy messages
func (p *ProxyTransferClient) SetWorkerID(workerID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.workerID = workerID
}

// TransferVolume returns a VolumeStream that wraps the proxy channel
func (p *ProxyTransferClient) TransferVolume(ctx context.Context) (VolumeStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, fmt.Errorf("proxy client is closed")
	}

	return &ProxyVolumeStream{
		stream:      p.stream,
		migrationID: p.migrationID,
		workerID:    p.workerID,
	}, nil
}

// TransferImageLayers returns an ImageStream that wraps the proxy channel
func (p *ProxyTransferClient) TransferImageLayers(ctx context.Context) (ImageStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, fmt.Errorf("proxy client is closed")
	}

	return &ProxyImageStream{
		stream:      p.stream,
		migrationID: p.migrationID,
		workerID:    p.workerID,
	}, nil
}

// Close closes the proxy channel and underlying connection
func (p *ProxyTransferClient) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	// Send close message through proxy
	closeMsg := &pb.ProxyData{
		MigrationId: p.migrationID,
		WorkerId:    p.workerID,
		Type:        pb.ProxyDataType_PROXY_DATA_CLOSE,
		Payload: &pb.ProxyData_Close{
			Close: &pb.ProxyClose{
				Success: true,
			},
		},
	}

	if err := p.stream.Send(closeMsg); err != nil {
		// Log but continue closing
		_ = err
	}

	if err := p.stream.CloseSend(); err != nil {
		// Log but continue closing
		_ = err
	}

	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

// ProxyVolumeStream adapts ProxyData to VolumeChunk interface
type ProxyVolumeStream struct {
	stream      pb.ProxyService_OpenProxyChannelClient
	migrationID string
	workerID    string

	mu sync.Mutex
}

// Send wraps VolumeChunk in ProxyData with type PROXY_DATA_VOLUME
func (s *ProxyVolumeStream) Send(chunk *pb.VolumeChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	proxyData := &pb.ProxyData{
		MigrationId: s.migrationID,
		WorkerId:    s.workerID,
		Type:        pb.ProxyDataType_PROXY_DATA_VOLUME,
		Payload: &pb.ProxyData_VolumeChunk{
			VolumeChunk: chunk,
		},
	}

	return s.stream.Send(proxyData)
}

// Recv unwraps TransferAck from ProxyData
func (s *ProxyVolumeStream) Recv() (*pb.TransferAck, error) {
	for {
		proxyData, err := s.stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("failed to receive proxy data: %w", err)
		}

		// Check if this is an ack message
		if proxyData.Type == pb.ProxyDataType_PROXY_DATA_ACK {
			if ack := proxyData.GetAck(); ack != nil {
				return ack, nil
			}
		}

		// Check for close message indicating error
		if proxyData.Type == pb.ProxyDataType_PROXY_DATA_CLOSE {
			if closeMsg := proxyData.GetClose(); closeMsg != nil {
				if !closeMsg.Success {
					return nil, fmt.Errorf("proxy channel closed with error: %s", closeMsg.Error)
				}
				return nil, io.EOF
			}
		}

		// Skip non-ack messages (they may be for other streams)
	}
}

// CloseSend signals end of sending on this stream
func (s *ProxyVolumeStream) CloseSend() error {
	// In proxy mode, we don't close the underlying stream
	// as it may be shared. The ProxyTransferClient.Close() handles this.
	// We could send a specific end-of-volume marker if needed.
	return nil
}

// ProxyImageStream adapts ProxyData to LayerBlob interface
type ProxyImageStream struct {
	stream      pb.ProxyService_OpenProxyChannelClient
	migrationID string
	workerID    string

	mu sync.Mutex
}

// Send wraps LayerBlob in ProxyData with type PROXY_DATA_IMAGE
func (s *ProxyImageStream) Send(blob *pb.LayerBlob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	proxyData := &pb.ProxyData{
		MigrationId: s.migrationID,
		WorkerId:    s.workerID,
		Type:        pb.ProxyDataType_PROXY_DATA_IMAGE,
		Payload: &pb.ProxyData_LayerBlob{
			LayerBlob: blob,
		},
	}

	return s.stream.Send(proxyData)
}

// Recv unwraps TransferAck from ProxyData
func (s *ProxyImageStream) Recv() (*pb.TransferAck, error) {
	for {
		proxyData, err := s.stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("failed to receive proxy data: %w", err)
		}

		// Check if this is an ack message
		if proxyData.Type == pb.ProxyDataType_PROXY_DATA_ACK {
			if ack := proxyData.GetAck(); ack != nil {
				return ack, nil
			}
		}

		// Check for close message indicating error
		if proxyData.Type == pb.ProxyDataType_PROXY_DATA_CLOSE {
			if closeMsg := proxyData.GetClose(); closeMsg != nil {
				if !closeMsg.Success {
					return nil, fmt.Errorf("proxy channel closed with error: %s", closeMsg.Error)
				}
				return nil, io.EOF
			}
		}

		// Skip non-ack messages (they may be for other streams)
	}
}

// CloseSend signals end of sending on this stream
func (s *ProxyImageStream) CloseSend() error {
	// In proxy mode, we don't close the underlying stream
	// as it may be shared. The ProxyTransferClient.Close() handles this.
	// We could send a specific end-of-image marker if needed.
	return nil
}
