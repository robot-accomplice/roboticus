package channel

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"golang.org/x/crypto/hkdf"

	"github.com/rs/zerolog/log"
)

// A2A protocol defaults.
const (
	DefaultA2AMaxMessageSize   = 64 * 1024
	DefaultA2ASessionTimeout   = 3600 // seconds
	DefaultA2ARateLimitPerPeer = 30
	DefaultA2ANonceTTL         = 300 // seconds
	DefaultA2AMaxSessions      = 256
)

// A2AConfig holds agent-to-agent communication parameters.
type A2AConfig struct {
	MaxMessageSize   int `mapstructure:"max_message_size"`    // bytes, default 64KB
	SessionTimeout   int `mapstructure:"session_timeout"`     // seconds, default 3600
	RateLimitPerPeer int `mapstructure:"rate_limit_per_peer"` // requests per 60s, default 30
	NonceTTL         int `mapstructure:"nonce_ttl"`           // seconds, default 300
	MaxSessions      int `mapstructure:"max_sessions"`        // default 256
}

// a2aSession represents an established encrypted session with a peer.
type a2aSession struct {
	peerID     string
	sessionKey []byte // AES-256-GCM key
	createdAt  time.Time
	lastActive time.Time
}

// a2aRateWindow tracks request timestamps for rate limiting.
type a2aRateWindow struct {
	timestamps []time.Time
}

// A2AAdapter implements Adapter for agent-to-agent encrypted communication.
// Uses X25519 ECDH for key agreement, HKDF-SHA256 for key derivation,
// and AES-256-GCM for authenticated encryption.
type A2AAdapter struct {
	cfg        A2AConfig
	privateKey *ecdh.PrivateKey
	publicKey  *ecdh.PublicKey

	mu          sync.Mutex
	sessions    map[string]*a2aSession
	rateWindows map[string]*a2aRateWindow
	seenNonces  map[string]time.Time
	inbound     chan InboundMessage
}

// NewA2AAdapter creates an agent-to-agent channel adapter.
func NewA2AAdapter(cfg A2AConfig) (*A2AAdapter, error) {
	if cfg.MaxMessageSize <= 0 {
		cfg.MaxMessageSize = DefaultA2AMaxMessageSize
	}
	if cfg.SessionTimeout <= 0 {
		cfg.SessionTimeout = DefaultA2ASessionTimeout
	}
	if cfg.RateLimitPerPeer <= 0 {
		cfg.RateLimitPerPeer = DefaultA2ARateLimitPerPeer
	}
	if cfg.NonceTTL <= 0 {
		cfg.NonceTTL = DefaultA2ANonceTTL
	}
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = DefaultA2AMaxSessions
	}

	// Generate X25519 key pair.
	curve := ecdh.X25519()
	privKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("a2a key generation: %w", err)
	}

	return &A2AAdapter{
		cfg:         cfg,
		privateKey:  privKey,
		publicKey:   privKey.PublicKey(),
		sessions:    make(map[string]*a2aSession),
		rateWindows: make(map[string]*a2aRateWindow),
		seenNonces:  make(map[string]time.Time),
		inbound:     make(chan InboundMessage, 64),
	}, nil
}

func (a *A2AAdapter) PlatformName() string { return "a2a" }

// PublicKeyHex returns the adapter's public key as hex for handshake exchange.
func (a *A2AAdapter) PublicKeyHex() string {
	return hex.EncodeToString(a.publicKey.Bytes())
}

// Recv returns the next decrypted inbound message.
func (a *A2AAdapter) Recv(ctx context.Context) (*InboundMessage, error) {
	select {
	case msg := <-a.inbound:
		return &msg, nil
	default:
		return nil, nil
	}
}

// Send encrypts and would transmit a message to a peer.
// The actual transport is handled externally; this prepares the encrypted payload.
func (a *A2AAdapter) Send(ctx context.Context, msg OutboundMessage) error {
	a.mu.Lock()
	session, ok := a.sessions[msg.RecipientID]
	a.mu.Unlock()

	if !ok {
		return fmt.Errorf("a2a: no session with peer %s", msg.RecipientID)
	}

	if len(msg.Content) > a.cfg.MaxMessageSize {
		return fmt.Errorf("a2a: message exceeds max size (%d > %d)", len(msg.Content), a.cfg.MaxMessageSize)
	}

	_, err := a.encrypt(session.sessionKey, []byte(msg.Content))
	if err != nil {
		return fmt.Errorf("a2a encrypt: %w", err)
	}

	a.mu.Lock()
	session.lastActive = time.Now()
	a.mu.Unlock()

	return nil
}

// EstablishSession performs ECDH key agreement with a peer.
func (a *A2AAdapter) EstablishSession(peerID string, peerPubKeyHex string, nonce string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Check rate limit.
	if !a.checkRateLimit(peerID) {
		return fmt.Errorf("a2a: rate limit exceeded for peer %s", peerID)
	}

	// Replay detection.
	if _, seen := a.seenNonces[nonce]; seen {
		return fmt.Errorf("a2a: nonce replay detected")
	}
	a.seenNonces[nonce] = time.Now()

	// Parse peer public key.
	peerPubBytes, err := hex.DecodeString(peerPubKeyHex)
	if err != nil {
		return fmt.Errorf("a2a: invalid peer public key: %w", err)
	}
	curve := ecdh.X25519()
	peerPubKey, err := curve.NewPublicKey(peerPubBytes)
	if err != nil {
		return fmt.Errorf("a2a: invalid peer public key: %w", err)
	}

	// ECDH key agreement.
	sharedSecret, err := a.privateKey.ECDH(peerPubKey)
	if err != nil {
		return fmt.Errorf("a2a: ECDH failed: %w", err)
	}

	// HKDF-SHA256 key derivation with domain-separated salt.
	salt := a.domainSalt(peerPubKey)
	sessionKey, err := deriveKey(sharedSecret, salt, 32)
	if err != nil {
		return fmt.Errorf("a2a: key derivation failed: %w", err)
	}

	// Evict oldest session if at capacity.
	if len(a.sessions) >= a.cfg.MaxSessions {
		a.evictOldestSession()
	}

	a.sessions[peerID] = &a2aSession{
		peerID:     peerID,
		sessionKey: sessionKey,
		createdAt:  time.Now(),
		lastActive: time.Now(),
	}

	log.Info().Str("peer", peerID).Msg("a2a session established")
	return nil
}

// DecryptInbound decrypts an incoming message and buffers it.
func (a *A2AAdapter) DecryptInbound(peerID string, ciphertext []byte) error {
	a.mu.Lock()
	session, ok := a.sessions[peerID]
	a.mu.Unlock()

	if !ok {
		return fmt.Errorf("a2a: no session with peer %s", peerID)
	}

	plaintext, err := a.decrypt(session.sessionKey, ciphertext)
	if err != nil {
		return fmt.Errorf("a2a decrypt: %w", err)
	}

	msg := InboundMessage{
		ID:        fmt.Sprintf("a2a-%d", time.Now().UnixNano()),
		Platform:  "a2a",
		SenderID:  peerID,
		ChatID:    peerID,
		Content:   string(plaintext),
		Timestamp: time.Now(),
	}

	select {
	case a.inbound <- msg:
	default:
		log.Warn().Msg("a2a: inbound buffer full")
	}

	a.mu.Lock()
	session.lastActive = time.Now()
	a.mu.Unlock()

	return nil
}

// domainSalt creates an order-independent salt from both public keys.
func (a *A2AAdapter) domainSalt(peerPubKey *ecdh.PublicKey) []byte {
	keys := []string{
		hex.EncodeToString(a.publicKey.Bytes()),
		hex.EncodeToString(peerPubKey.Bytes()),
	}
	sort.Strings(keys)
	h := sha256.Sum256([]byte("goboticus-a2a:" + keys[0] + ":" + keys[1]))
	return h[:]
}

func deriveKey(secret, salt []byte, length int) ([]byte, error) {
	info := []byte("goboticus-a2a-session-key")
	reader := hkdf.New(sha256.New, secret, salt, info)
	key := make([]byte, length)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

func (a *A2AAdapter) encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	// nonce || ciphertext
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (a *A2AAdapter) decrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
}

func (a *A2AAdapter) checkRateLimit(peerID string) bool {
	now := time.Now()
	window, ok := a.rateWindows[peerID]
	if !ok {
		window = &a2aRateWindow{}
		a.rateWindows[peerID] = window
	}

	// Prune old entries (60s window).
	cutoff := now.Add(-60 * time.Second)
	fresh := window.timestamps[:0]
	for _, t := range window.timestamps {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	window.timestamps = fresh

	if len(window.timestamps) >= a.cfg.RateLimitPerPeer {
		return false
	}
	window.timestamps = append(window.timestamps, now)
	return true
}

func (a *A2AAdapter) evictOldestSession() {
	var oldestID string
	var oldestTime time.Time
	for id, s := range a.sessions {
		if oldestID == "" || s.lastActive.Before(oldestTime) {
			oldestID = id
			oldestTime = s.lastActive
		}
	}
	if oldestID != "" {
		delete(a.sessions, oldestID)
		log.Debug().Str("peer", oldestID).Msg("a2a: evicted oldest session")
	}
}

// CleanupExpired removes stale sessions and nonces.
func (a *A2AAdapter) CleanupExpired() {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	timeout := time.Duration(a.cfg.SessionTimeout) * time.Second
	nonceTTL := time.Duration(a.cfg.NonceTTL) * time.Second

	for id, s := range a.sessions {
		if now.Sub(s.lastActive) > timeout {
			delete(a.sessions, id)
		}
	}
	for nonce, t := range a.seenNonces {
		if now.Sub(t) > nonceTTL {
			delete(a.seenNonces, nonce)
		}
	}
}
