package peer

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base32"
	"encoding/pem"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/config"
	"github.com/artemis/docker-migrate/internal/observability"
	"go.uber.org/zap"
)

const (
	// Pairing code parameters
	PairingCodeLength = 6 // 6 character alphanumeric codes
	PairingTimeout    = 5 * time.Minute

	// Rate limiting
	MaxAttemptsPerMinute = 5
	BanDuration          = 15 * time.Minute
)

// PairingManager handles secure peer pairing with rate limiting
// Uses X25519 ECDH key exchange with pairing code as verification
type PairingManager struct {
	activeSessions map[string]*PairingSession
	trustedPeers   map[string]*TrustedPeer
	attempts       map[string]*rateLimitTracker
	config         *config.Config
	crypto         *CryptoManager
	logger         *observability.Logger
	mu             sync.RWMutex
}

// PairingSession represents an active pairing session
type PairingSession struct {
	Code         string
	CodeHash     []byte // SHA-256 hash of code for verification
	ExpiresAt    time.Time
	PrivateKey   *ecdh.PrivateKey // X25519 ephemeral key
	PublicKey    []byte           // Our public key to send
	PeerPublic   []byte           // Peer's public key
	SharedSecret []byte           // Derived shared secret
	PeerCert     *x509.Certificate
	PeerAddress  string
	Role         PairingRole
	Created      time.Time
	Completed    bool
}

// PairingRole identifies the role in key exchange
type PairingRole int

const (
	RoleInitiator PairingRole = iota
	RoleResponder
)

// TrustedPeer represents a successfully paired peer
type TrustedPeer struct {
	ID          string
	Name        string
	PublicKey   []byte
	Fingerprint string
	FirstSeen   time.Time
	LastSeen    time.Time
	Address     string
	Certificate *x509.Certificate
}

// rateLimitTracker tracks pairing attempts for rate limiting
type rateLimitTracker struct {
	attempts     int
	firstAttempt time.Time
	bannedUntil  time.Time
}

// PairingMessage is exchanged during pairing
type PairingMessage struct {
	PublicKey    []byte `json:"public_key"`
	CodeVerifier []byte `json:"code_verifier"` // HMAC of public key using code hash
	Certificate  []byte `json:"certificate"`   // PEM encoded certificate
}

// NewPairingManager creates a new pairing manager
func NewPairingManager(cfg *config.Config, crypto *CryptoManager, logger *observability.Logger) *PairingManager {
	pm := &PairingManager{
		activeSessions: make(map[string]*PairingSession),
		trustedPeers:   make(map[string]*TrustedPeer),
		attempts:       make(map[string]*rateLimitTracker),
		config:         cfg,
		crypto:         crypto,
		logger:         logger,
	}

	// Load trusted peers from config
	for _, peer := range cfg.ListTrustedPeers() {
		pm.trustedPeers[peer.ID] = &TrustedPeer{
			ID:          peer.ID,
			Name:        peer.Name,
			Fingerprint: peer.Fingerprint,
			FirstSeen:   peer.AddedAt,
			LastSeen:    peer.LastSeen,
			Address:     peer.Address,
		}
	}

	// Start cleanup goroutine
	go pm.cleanupExpiredSessions()

	return pm
}

// GeneratePairingCode creates a 6-char alphanumeric code valid for 5 minutes
func (pm *PairingManager) GeneratePairingCode() (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Generate random 6-character code
	code, err := generateRandomCode(PairingCodeLength)
	if err != nil {
		return "", fmt.Errorf("failed to generate pairing code: %w", err)
	}

	// Generate ephemeral X25519 key pair
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to generate ephemeral key: %w", err)
	}

	// Hash the code for verification
	codeHash := sha256.Sum256([]byte(code))

	// Store session
	session := &PairingSession{
		Code:       code,
		CodeHash:   codeHash[:],
		ExpiresAt:  time.Now().Add(PairingTimeout),
		PrivateKey: privateKey,
		PublicKey:  privateKey.PublicKey().Bytes(),
		Role:       RoleInitiator,
		Created:    time.Now(),
		Completed:  false,
	}

	pm.activeSessions[code] = session

	pm.logger.Info("generated pairing code",
		zap.String("code", code),
		zap.Time("expires_at", session.ExpiresAt),
	)

	return code, nil
}

// GetPairingMessage returns the message to send during pairing
func (pm *PairingManager) GetPairingMessage(code string) (*PairingMessage, error) {
	pm.mu.RLock()
	session, exists := pm.activeSessions[code]
	pm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("invalid or expired pairing code")
	}

	if time.Now().After(session.ExpiresAt) {
		pm.mu.Lock()
		delete(pm.activeSessions, code)
		pm.mu.Unlock()
		return nil, fmt.Errorf("pairing code expired")
	}

	// Create code verifier: HMAC(public_key, code_hash)
	verifier := computeCodeVerifier(session.PublicKey, session.CodeHash)

	// Get our certificate
	certPEM := pm.crypto.GetCertificatePEM()

	return &PairingMessage{
		PublicKey:    session.PublicKey,
		CodeVerifier: verifier,
		Certificate:  certPEM,
	}, nil
}

// AcceptPairing handles incoming pairing request
func (pm *PairingManager) AcceptPairing(code string, peerAddress string, peerMsg *PairingMessage) (*PairingMessage, error) {
	// Check rate limiting
	if err := pm.checkRateLimit(peerAddress); err != nil {
		return nil, err
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Record attempt
	pm.recordAttempt(peerAddress)

	// Hash the provided code
	codeHash := sha256.Sum256([]byte(code))

	// Verify the peer's code verifier
	expectedVerifier := computeCodeVerifier(peerMsg.PublicKey, codeHash[:])
	if !secureCompare(peerMsg.CodeVerifier, expectedVerifier) {
		return nil, fmt.Errorf("invalid pairing code")
	}

	// Generate our ephemeral key pair
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ephemeral key: %w", err)
	}

	// Import peer's public key
	peerPubKey, err := ecdh.X25519().NewPublicKey(peerMsg.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("invalid peer public key: %w", err)
	}

	// Compute shared secret
	sharedSecret, err := privateKey.ECDH(peerPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute shared secret: %w", err)
	}

	// Parse peer certificate
	peerCert, err := parseCertificatePEM(peerMsg.Certificate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse peer certificate: %w", err)
	}

	// Create session for this responder
	session := &PairingSession{
		Code:         code,
		CodeHash:     codeHash[:],
		ExpiresAt:    time.Now().Add(PairingTimeout),
		PrivateKey:   privateKey,
		PublicKey:    privateKey.PublicKey().Bytes(),
		PeerPublic:   peerMsg.PublicKey,
		SharedSecret: sharedSecret,
		PeerCert:     peerCert,
		PeerAddress:  peerAddress,
		Role:         RoleResponder,
		Created:      time.Now(),
		Completed:    false,
	}

	pm.activeSessions[code+"-responder"] = session

	// Create our response
	verifier := computeCodeVerifier(session.PublicKey, codeHash[:])
	certPEM := pm.crypto.GetCertificatePEM()

	pm.logger.Info("accepted pairing request",
		zap.String("peer_address", peerAddress),
	)

	return &PairingMessage{
		PublicKey:    session.PublicKey,
		CodeVerifier: verifier,
		Certificate:  certPEM,
	}, nil
}

// CompletePairing finishes the pairing and establishes trust
func (pm *PairingManager) CompletePairing(code string, peerMsg *PairingMessage) (*TrustedPeer, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	session, exists := pm.activeSessions[code]
	if !exists {
		return nil, fmt.Errorf("invalid or expired pairing code")
	}

	if time.Now().After(session.ExpiresAt) {
		delete(pm.activeSessions, code)
		return nil, fmt.Errorf("pairing code expired")
	}

	if session.Completed {
		return nil, fmt.Errorf("pairing already completed")
	}

	// Verify peer's code verifier
	expectedVerifier := computeCodeVerifier(peerMsg.PublicKey, session.CodeHash)
	if !secureCompare(peerMsg.CodeVerifier, expectedVerifier) {
		return nil, fmt.Errorf("invalid pairing verification")
	}

	// Import peer's public key and compute shared secret
	peerPubKey, err := ecdh.X25519().NewPublicKey(peerMsg.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("invalid peer public key: %w", err)
	}

	sharedSecret, err := session.PrivateKey.ECDH(peerPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute shared secret: %w", err)
	}

	// Parse peer certificate
	peerCert, err := parseCertificatePEM(peerMsg.Certificate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse peer certificate: %w", err)
	}

	// Derive session key
	sessionKey, err := pm.crypto.DeriveSessionKey(sharedSecret, session.CodeHash)
	if err != nil {
		return nil, fmt.Errorf("failed to derive session key: %w", err)
	}

	// Compute certificate fingerprint
	fingerprint := ComputeFingerprint(peerCert)

	// Create trusted peer
	peerID := generatePeerID(peerCert)
	trustedPeer := &TrustedPeer{
		ID:          peerID,
		Name:        peerCert.Subject.CommonName,
		PublicKey:   sessionKey,
		Fingerprint: fingerprint,
		FirstSeen:   time.Now(),
		LastSeen:    time.Now(),
		Address:     session.PeerAddress,
		Certificate: peerCert,
	}

	// Store trusted peer
	pm.trustedPeers[peerID] = trustedPeer

	// Add certificate to crypto manager's trusted store
	if err := pm.crypto.AddTrustedCert(peerCert); err != nil {
		return nil, fmt.Errorf("failed to add trusted certificate: %w", err)
	}

	// Save to config
	pm.config.AddTrustedPeer(&config.TrustedPeer{
		ID:          peerID,
		Name:        trustedPeer.Name,
		Fingerprint: fingerprint,
		Address:     session.PeerAddress,
		AddedAt:     trustedPeer.FirstSeen,
		LastSeen:    trustedPeer.LastSeen,
	})

	if err := pm.config.Save(""); err != nil {
		pm.logger.Warn("failed to save config", zap.Error(err))
	}

	// Mark session as completed
	session.Completed = true
	session.SharedSecret = sharedSecret
	session.PeerCert = peerCert

	pm.logger.Info("pairing completed",
		zap.String("peer_id", peerID),
		zap.String("fingerprint", fingerprint),
	)

	// Clean up session after delay
	go func() {
		time.Sleep(1 * time.Minute)
		pm.mu.Lock()
		delete(pm.activeSessions, code)
		pm.mu.Unlock()
	}()

	return trustedPeer, nil
}

// GetTrustedPeer retrieves a trusted peer by ID
func (pm *PairingManager) GetTrustedPeer(peerID string) (*TrustedPeer, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	peer, ok := pm.trustedPeers[peerID]
	return peer, ok
}

// RemoveTrustedPeer removes a peer from trusted list
func (pm *PairingManager) RemoveTrustedPeer(peerID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	peer, ok := pm.trustedPeers[peerID]
	if !ok {
		return fmt.Errorf("peer not found")
	}

	// Remove from trusted certs
	pm.crypto.RemoveTrustedCert(peer.Fingerprint)

	// Remove from config
	pm.config.RemoveTrustedPeer(peerID)

	// Save config
	if err := pm.config.Save(""); err != nil {
		pm.logger.Warn("failed to save config", zap.Error(err))
	}

	delete(pm.trustedPeers, peerID)

	pm.logger.Info("removed trusted peer",
		zap.String("peer_id", peerID),
	)

	return nil
}

// ListTrustedPeers returns all trusted peers
func (pm *PairingManager) ListTrustedPeers() []*TrustedPeer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	peers := make([]*TrustedPeer, 0, len(pm.trustedPeers))
	for _, peer := range pm.trustedPeers {
		peers = append(peers, peer)
	}

	return peers
}

// UpdatePeerLastSeen updates the last seen timestamp for a peer
func (pm *PairingManager) UpdatePeerLastSeen(peerID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if peer, ok := pm.trustedPeers[peerID]; ok {
		peer.LastSeen = time.Now()
		pm.config.UpdatePeerLastSeen(peerID)
	}
}

// checkRateLimit checks if an address is rate limited
func (pm *PairingManager) checkRateLimit(address string) error {
	pm.mu.RLock()
	tracker, exists := pm.attempts[address]
	pm.mu.RUnlock()

	if !exists {
		return nil
	}

	// Check if banned
	if time.Now().Before(tracker.bannedUntil) {
		return fmt.Errorf("too many failed attempts, banned until %s", tracker.bannedUntil.Format(time.RFC3339))
	}

	// Reset if outside window
	if time.Since(tracker.firstAttempt) > time.Minute {
		pm.mu.Lock()
		delete(pm.attempts, address)
		pm.mu.Unlock()
		return nil
	}

	// Check attempt count
	if tracker.attempts >= MaxAttemptsPerMinute {
		pm.mu.Lock()
		tracker.bannedUntil = time.Now().Add(BanDuration)
		pm.mu.Unlock()

		pm.logger.Warn("rate limit exceeded, banning address",
			zap.String("address", address),
			zap.Time("banned_until", tracker.bannedUntil),
		)

		return fmt.Errorf("too many failed attempts, banned for %s", BanDuration)
	}

	return nil
}

// recordAttempt records a pairing attempt
func (pm *PairingManager) recordAttempt(address string) {
	tracker, exists := pm.attempts[address]
	if !exists {
		pm.attempts[address] = &rateLimitTracker{
			attempts:     1,
			firstAttempt: time.Now(),
		}
		return
	}

	// Reset if outside window
	if time.Since(tracker.firstAttempt) > time.Minute {
		tracker.attempts = 1
		tracker.firstAttempt = time.Now()
		return
	}

	tracker.attempts++
}

// cleanupExpiredSessions removes expired pairing sessions
func (pm *PairingManager) cleanupExpiredSessions() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		pm.mu.Lock()
		now := time.Now()

		for code, session := range pm.activeSessions {
			if now.After(session.ExpiresAt) {
				delete(pm.activeSessions, code)
				pm.logger.Debug("cleaned up expired session",
					zap.String("code", code),
				)
			}
		}

		// Clean up rate limit trackers
		for addr, tracker := range pm.attempts {
			if now.After(tracker.bannedUntil) && time.Since(tracker.firstAttempt) > time.Minute {
				delete(pm.attempts, addr)
			}
		}

		pm.mu.Unlock()
	}
}

// computeCodeVerifier creates HMAC-like verifier
func computeCodeVerifier(publicKey, codeHash []byte) []byte {
	// Combine public key and code hash
	combined := append(publicKey, codeHash...)
	hash := sha256.Sum256(combined)
	return hash[:]
}

// secureCompare performs constant-time comparison
func secureCompare(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

// parseCertificatePEM parses a PEM-encoded certificate
func parseCertificatePEM(pemData []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

// generateRandomCode generates a random alphanumeric code
func generateRandomCode(length int) (string, error) {
	// Use base32 for human-readable codes (uppercase, no ambiguous characters)
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Encode to base32 and take first 'length' characters
	encoded := base32.StdEncoding.EncodeToString(bytes)
	code := strings.ToUpper(encoded[:length])

	// Remove ambiguous characters (0, O, I, 1)
	code = strings.ReplaceAll(code, "0", "8")
	code = strings.ReplaceAll(code, "O", "9")
	code = strings.ReplaceAll(code, "I", "J")
	code = strings.ReplaceAll(code, "1", "7")

	return code, nil
}

// generatePeerID generates a unique peer ID from certificate
func generatePeerID(cert *x509.Certificate) string {
	hash := sha256.Sum256(cert.Raw)
	return fmt.Sprintf("peer-%x", hash[:8])
}
