package peer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/config"
	"github.com/artemis/docker-migrate/internal/observability"
	"go.uber.org/zap"
)

// PeerStatus represents the online/offline status of a peer
type PeerStatus int

const (
	PeerOffline PeerStatus = iota
	PeerConnecting
	PeerOnline
)

func (ps PeerStatus) String() string {
	switch ps {
	case PeerOffline:
		return "offline"
	case PeerConnecting:
		return "connecting"
	case PeerOnline:
		return "online"
	default:
		return "unknown"
	}
}

// ConnectionType identifies how the peer is connected
type ConnectionType int

const (
	ConnectionDirect ConnectionType = iota
	ConnectionTailscale
	ConnectionWireGuard
	ConnectionTURN
)

func (ct ConnectionType) String() string {
	switch ct {
	case ConnectionDirect:
		return "direct"
	case ConnectionTailscale:
		return "tailscale"
	case ConnectionWireGuard:
		return "wireguard"
	case ConnectionTURN:
		return "turn"
	default:
		return "unknown"
	}
}

// Peer represents a discovered or paired peer
type Peer struct {
	ID           string
	Name         string
	Address      string
	Status       PeerStatus
	LastSeen     time.Time
	Connection   ConnectionType
	Latency      time.Duration
	Fingerprint  string
}

// PeerDiscovery handles peer discovery and health checking
type PeerDiscovery struct {
	localPeer    *Peer
	knownPeers   map[string]*Peer
	config       *config.Config
	pairing      *PairingManager
	crypto       *CryptoManager
	logger       *observability.Logger
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewPeerDiscovery creates a new peer discovery service
func NewPeerDiscovery(
	cfg *config.Config,
	pairing *PairingManager,
	crypto *CryptoManager,
	logger *observability.Logger,
) *PeerDiscovery {
	ctx, cancel := context.WithCancel(context.Background())

	localPeer := &Peer{
		ID:          fmt.Sprintf("local-%s", crypto.GetFingerprint()[:8]),
		Name:        "local",
		Status:      PeerOnline,
		LastSeen:    time.Now(),
		Connection:  ConnectionDirect,
		Fingerprint: crypto.GetFingerprint(),
	}

	pd := &PeerDiscovery{
		localPeer:  localPeer,
		knownPeers: make(map[string]*Peer),
		config:     cfg,
		pairing:    pairing,
		crypto:     crypto,
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Load trusted peers
	for _, trustedPeer := range pairing.ListTrustedPeers() {
		pd.knownPeers[trustedPeer.ID] = &Peer{
			ID:          trustedPeer.ID,
			Name:        trustedPeer.Name,
			Address:     trustedPeer.Address,
			Status:      PeerOffline,
			LastSeen:    trustedPeer.LastSeen,
			Connection:  ConnectionDirect,
			Fingerprint: trustedPeer.Fingerprint,
		}
	}

	return pd
}

// Start starts the discovery service
func (pd *PeerDiscovery) Start(ctx context.Context) error {
	pd.logger.Info("starting peer discovery service")

	// Start health check goroutine
	go pd.StartHealthCheck(pd.ctx)

	return nil
}

// Stop stops the discovery service
func (pd *PeerDiscovery) Stop() error {
	pd.logger.Info("stopping peer discovery service")
	pd.cancel()
	return nil
}

// RegisterPeer adds a peer from pairing
func (pd *PeerDiscovery) RegisterPeer(trustedPeer *TrustedPeer) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	peer := &Peer{
		ID:          trustedPeer.ID,
		Name:        trustedPeer.Name,
		Address:     trustedPeer.Address,
		Status:      PeerOffline,
		LastSeen:    trustedPeer.LastSeen,
		Connection:  ConnectionDirect,
		Fingerprint: trustedPeer.Fingerprint,
	}

	pd.knownPeers[trustedPeer.ID] = peer

	pd.logger.Info("registered peer",
		zap.String("peer_id", peer.ID),
		zap.String("name", peer.Name),
		zap.String("address", peer.Address),
	)

	return nil
}

// GetOnlinePeers returns currently reachable peers
func (pd *PeerDiscovery) GetOnlinePeers() []*Peer {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	online := make([]*Peer, 0)
	for _, peer := range pd.knownPeers {
		if peer.Status == PeerOnline {
			online = append(online, peer)
		}
	}

	return online
}

// GetAllPeers returns all known peers
func (pd *PeerDiscovery) GetAllPeers() []*Peer {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	peers := make([]*Peer, 0, len(pd.knownPeers))
	for _, peer := range pd.knownPeers {
		peers = append(peers, peer)
	}

	return peers
}

// GetPeer retrieves a specific peer by ID
func (pd *PeerDiscovery) GetPeer(peerID string) (*Peer, bool) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	peer, ok := pd.knownPeers[peerID]
	return peer, ok
}

// StartHealthCheck runs periodic health checks on all peers
func (pd *PeerDiscovery) StartHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	pd.logger.Info("health check loop started")

	for {
		select {
		case <-ctx.Done():
			pd.logger.Info("health check loop stopped")
			return
		case <-ticker.C:
			pd.checkPeerHealth()
		}
	}
}

// checkPeerHealth pings all known peers to check their status
func (pd *PeerDiscovery) checkPeerHealth() {
	pd.mu.RLock()
	peers := make([]*Peer, 0, len(pd.knownPeers))
	for _, peer := range pd.knownPeers {
		peers = append(peers, peer)
	}
	pd.mu.RUnlock()

	for _, peer := range peers {
		go pd.checkSinglePeer(peer)
	}
}

// checkSinglePeer checks health of a single peer
func (pd *PeerDiscovery) checkSinglePeer(peer *Peer) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try to create client and ping
	client, err := NewGRPCClient(
		peer.Address,
		peer.Fingerprint,
		nil, // No transfer manager needed for ping
		pd.crypto,
		pd.logger,
	)
	if err != nil {
		pd.updatePeerStatus(peer.ID, PeerOffline, 0)
		return
	}
	defer client.Close()

	_, latency, err := client.Ping(ctx)
	if err != nil {
		pd.updatePeerStatus(peer.ID, PeerOffline, 0)
		return
	}

	pd.updatePeerStatus(peer.ID, PeerOnline, latency)
	pd.pairing.UpdatePeerLastSeen(peer.ID)
}

// updatePeerStatus updates the status of a peer
func (pd *PeerDiscovery) updatePeerStatus(peerID string, status PeerStatus, latency time.Duration) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	peer, ok := pd.knownPeers[peerID]
	if !ok {
		return
	}

	oldStatus := peer.Status
	peer.Status = status
	peer.Latency = latency

	if status == PeerOnline {
		peer.LastSeen = time.Now()
	}

	if oldStatus != status {
		pd.logger.Info("peer status changed",
			zap.String("peer_id", peerID),
			zap.String("old_status", oldStatus.String()),
			zap.String("new_status", status.String()),
			zap.Duration("latency", latency),
		)
	}
}

// RemovePeer removes a peer from known peers
func (pd *PeerDiscovery) RemovePeer(peerID string) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if _, ok := pd.knownPeers[peerID]; !ok {
		return fmt.Errorf("peer not found: %s", peerID)
	}

	delete(pd.knownPeers, peerID)

	pd.logger.Info("removed peer",
		zap.String("peer_id", peerID),
	)

	return nil
}
