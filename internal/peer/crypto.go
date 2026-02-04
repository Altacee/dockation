package peer

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/observability"
	"go.uber.org/zap"
	"golang.org/x/crypto/hkdf"
)

// CryptoManager handles all cryptographic operations including
// key generation, certificate management, and SPAKE2+ key derivation
type CryptoManager struct {
	privateKey    *ecdsa.PrivateKey
	certificate   *x509.Certificate
	certPEM       []byte
	trustedCerts  map[string]*x509.Certificate
	certPath      string
	keyPath       string
	logger        *observability.Logger
	mu            sync.RWMutex
}

// NewCryptoManager creates a new crypto manager
func NewCryptoManager(logger *observability.Logger, certDir string) (*CryptoManager, error) {
	if certDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		certDir = filepath.Join(homeDir, ".docker-migrate", "certs")
	}

	// Ensure cert directory exists
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cert directory: %w", err)
	}

	cm := &CryptoManager{
		trustedCerts: make(map[string]*x509.Certificate),
		certPath:     filepath.Join(certDir, "server.crt"),
		keyPath:      filepath.Join(certDir, "server.key"),
		logger:       logger,
	}

	// Try to load existing keypair
	if err := cm.loadOrGenerateKeypair(); err != nil {
		return nil, fmt.Errorf("failed to initialize keypair: %w", err)
	}

	logger.Info("crypto manager initialized",
		zap.String("fingerprint", cm.GetFingerprint()),
	)

	return cm, nil
}

// loadOrGenerateKeypair loads existing keypair or generates a new one
func (cm *CryptoManager) loadOrGenerateKeypair() error {
	// Check if cert and key exist
	if _, err := os.Stat(cm.certPath); os.IsNotExist(err) {
		cm.logger.Info("generating new keypair and certificate")
		return cm.generateAndSaveKeypair()
	}

	// Load existing keypair
	certPEM, err := os.ReadFile(cm.certPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate: %w", err)
	}

	keyPEM, err := os.ReadFile(cm.keyPath)
	if err != nil {
		return fmt.Errorf("failed to read private key: %w", err)
	}

	// Parse certificate
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("failed to parse certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Parse private key
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("failed to parse private key PEM")
	}

	privateKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	// Verify certificate hasn't expired
	if time.Now().After(cert.NotAfter) {
		cm.logger.Warn("certificate expired, generating new one")
		return cm.generateAndSaveKeypair()
	}

	cm.certificate = cert
	cm.privateKey = privateKey
	cm.certPEM = certPEM

	cm.logger.Info("loaded existing keypair",
		zap.Time("expires", cert.NotAfter),
	)

	return nil
}

// generateAndSaveKeypair generates a new ECDSA keypair and self-signed certificate
func (cm *CryptoManager) generateAndSaveKeypair() error {
	// Generate ECDSA P-256 keypair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create self-signed certificate valid for 1 year
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Docker Migrate"},
			CommonName:   "docker-migrate",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return fmt.Errorf("failed to parse created certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})

	// Atomic write: write to temp files then rename
	certTmp := cm.certPath + ".tmp"
	keyTmp := cm.keyPath + ".tmp"

	if err := os.WriteFile(certTmp, certPEM, 0600); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	if err := os.WriteFile(keyTmp, keyPEM, 0600); err != nil {
		os.Remove(certTmp)
		return fmt.Errorf("failed to write private key: %w", err)
	}

	// Atomic rename
	if err := os.Rename(certTmp, cm.certPath); err != nil {
		os.Remove(certTmp)
		os.Remove(keyTmp)
		return fmt.Errorf("failed to rename certificate: %w", err)
	}

	if err := os.Rename(keyTmp, cm.keyPath); err != nil {
		os.Remove(keyTmp)
		return fmt.Errorf("failed to rename private key: %w", err)
	}

	cm.certificate = cert
	cm.privateKey = privateKey
	cm.certPEM = certPEM

	return nil
}

// GetFingerprint returns SHA-256 fingerprint of the certificate
func (cm *CryptoManager) GetFingerprint() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.certificate == nil {
		return ""
	}

	hash := sha256.Sum256(cm.certificate.Raw)
	return hex.EncodeToString(hash[:])
}

// DeriveSessionKey derives a session key from SPAKE2+ shared secret using HKDF
func (cm *CryptoManager) DeriveSessionKey(spakeSecret []byte, salt []byte) ([]byte, error) {
	if len(spakeSecret) == 0 {
		return nil, fmt.Errorf("spake secret cannot be empty")
	}

	// Use HKDF-SHA256 to derive 32-byte key
	hkdfReader := hkdf.New(sha256.New, spakeSecret, salt, []byte("docker-migrate-session-key-v1"))
	sessionKey := make([]byte, 32)

	if _, err := hkdfReader.Read(sessionKey); err != nil {
		return nil, fmt.Errorf("failed to derive session key: %w", err)
	}

	return sessionKey, nil
}

// TLSConfig returns TLS configuration for server
func (cm *CryptoManager) TLSConfig() (*tls.Config, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.certificate == nil || cm.privateKey == nil {
		return nil, fmt.Errorf("certificate or private key not initialized")
	}

	cert := tls.Certificate{
		Certificate: [][]byte{cm.certificate.Raw},
		PrivateKey:  cm.privateKey,
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_AES_128_GCM_SHA256,
		},
		PreferServerCipherSuites: true,
		ClientAuth:               tls.RequireAnyClientCert,
		VerifyPeerCertificate:    cm.verifyPeerCertificate,
	}

	return config, nil
}

// TLSClientConfig returns TLS configuration for client
func (cm *CryptoManager) TLSClientConfig(expectedFingerprint string) (*tls.Config, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.certificate == nil || cm.privateKey == nil {
		return nil, fmt.Errorf("certificate or private key not initialized")
	}

	cert := tls.Certificate{
		Certificate: [][]byte{cm.certificate.Raw},
		PrivateKey:  cm.privateKey,
	}

	config := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true, // We do manual verification via fingerprint
		MinVersion:         tls.VersionTLS13,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no peer certificate provided")
			}

			// Verify fingerprint
			hash := sha256.Sum256(rawCerts[0])
			fingerprint := hex.EncodeToString(hash[:])

			if fingerprint != expectedFingerprint {
				return fmt.Errorf("peer certificate fingerprint mismatch: expected %s, got %s",
					expectedFingerprint, fingerprint)
			}

			return nil
		},
	}

	return config, nil
}

// verifyPeerCertificate verifies peer certificate against trusted store
func (cm *CryptoManager) verifyPeerCertificate(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return fmt.Errorf("no peer certificate provided")
	}

	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("failed to parse peer certificate: %w", err)
	}

	// Check if certificate has expired
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("peer certificate not yet valid")
	}
	if now.After(cert.NotAfter) {
		return fmt.Errorf("peer certificate expired")
	}

	// Calculate fingerprint
	hash := sha256.Sum256(cert.Raw)
	fingerprint := hex.EncodeToString(hash[:])

	// Check against trusted certs
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if _, trusted := cm.trustedCerts[fingerprint]; !trusted {
		return fmt.Errorf("peer certificate not in trusted store: %s", fingerprint)
	}

	return nil
}

// AddTrustedCert adds a certificate to the trusted store
func (cm *CryptoManager) AddTrustedCert(cert *x509.Certificate) error {
	if cert == nil {
		return fmt.Errorf("certificate cannot be nil")
	}

	hash := sha256.Sum256(cert.Raw)
	fingerprint := hex.EncodeToString(hash[:])

	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.trustedCerts[fingerprint] = cert

	cm.logger.Info("added trusted certificate",
		zap.String("fingerprint", fingerprint),
		zap.String("subject", cert.Subject.CommonName),
	)

	return nil
}

// RemoveTrustedCert removes a certificate from the trusted store
func (cm *CryptoManager) RemoveTrustedCert(fingerprint string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.trustedCerts, fingerprint)

	cm.logger.Info("removed trusted certificate",
		zap.String("fingerprint", fingerprint),
	)
}

// IsTrusted checks if a certificate fingerprint is trusted
func (cm *CryptoManager) IsTrusted(fingerprint string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	_, trusted := cm.trustedCerts[fingerprint]
	return trusted
}

// GetCertificate returns the current certificate
func (cm *CryptoManager) GetCertificate() *x509.Certificate {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.certificate
}

// GetCertificatePEM returns the certificate in PEM format
func (cm *CryptoManager) GetCertificatePEM() []byte {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.certPEM
}

// ComputeFingerprint computes SHA-256 fingerprint of a certificate
func ComputeFingerprint(cert *x509.Certificate) string {
	hash := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(hash[:])
}

// ComputeFingerprintFromPEM computes fingerprint from PEM-encoded certificate
func ComputeFingerprintFromPEM(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", fmt.Errorf("failed to parse certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse certificate: %w", err)
	}

	return ComputeFingerprint(cert), nil
}

// HashPassword creates a SHA-256 hash suitable for SPAKE2+
func HashPassword(password string) []byte {
	hash := sha256.Sum256([]byte(password))
	return hash[:]
}

// GetServerTLSConfig returns TLS configuration for server (alias for TLSConfig)
func (cm *CryptoManager) GetServerTLSConfig() (*tls.Config, error) {
	return cm.TLSConfig()
}

// GetClientTLSConfig returns TLS configuration for client without fingerprint verification
// This is used when connecting to servers where fingerprint is not known in advance
func (cm *CryptoManager) GetClientTLSConfig() (*tls.Config, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.certificate == nil || cm.privateKey == nil {
		return nil, fmt.Errorf("certificate or private key not initialized")
	}

	cert := tls.Certificate{
		Certificate: [][]byte{cm.certificate.Raw},
		PrivateKey:  cm.privateKey,
	}

	config := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: false, // Will be set by caller if needed
		MinVersion:         tls.VersionTLS13,
	}

	return config, nil
}
