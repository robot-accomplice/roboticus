// DeviceManager manages device pairing, trust, and cryptographic identity.
//
// Ported from Rust: crates/roboticus-agent/src/device.rs
//
// State machine: Pending → Verified (accept) or Rejected (reject).
// Only Verified devices are trusted for cross-device operations.

package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// PairingState tracks a device's pairing lifecycle.
type PairingState int

const (
	PairingPending PairingState = iota
	PairingVerified
	PairingRejected
	PairingExpired
)

func (s PairingState) String() string {
	switch s {
	case PairingPending:
		return "pending"
	case PairingVerified:
		return "verified"
	case PairingRejected:
		return "rejected"
	case PairingExpired:
		return "expired"
	default:
		return "unknown"
	}
}

// DeviceIdentity holds this device's cryptographic identity.
type DeviceIdentity struct {
	DeviceID     string
	PublicKeyHex string
	CreatedAt    time.Time
	DeviceName   string
	privateKey   *ecdsa.PrivateKey
}

// PairedDevice represents a known remote device.
type PairedDevice struct {
	DeviceID     string
	PublicKeyHex string
	DeviceName   string
	State        PairingState
	PairedAt     *time.Time
	LastSeen     *time.Time
}

// DeviceManager manages device pairing state.
type DeviceManager struct {
	mu            sync.RWMutex
	identity      DeviceIdentity
	pairedDevices map[string]*PairedDevice
	maxPaired     int
}

// GenerateDeviceIdentity creates a new ECDSA device identity.
func GenerateDeviceIdentity(deviceName string) (*DeviceIdentity, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate device key: %w", err)
	}

	// Use ECDH conversion to avoid deprecated direct X/Y field access (Go 1.26).
	ecdhKey, err := key.PublicKey.ECDH()
	if err != nil {
		return nil, fmt.Errorf("failed to convert public key: %w", err)
	}
	pubBytes := ecdhKey.Bytes()
	deviceID := "dev_" + randomHex(16)

	return &DeviceIdentity{
		DeviceID:     deviceID,
		PublicKeyHex: hex.EncodeToString(pubBytes),
		CreatedAt:    time.Now(),
		DeviceName:   deviceName,
		privateKey:   key,
	}, nil
}

// Fingerprint returns a short identifier derived from the public key.
func (id *DeviceIdentity) Fingerprint() string {
	h := sha256.Sum256([]byte(id.PublicKeyHex))
	return hex.EncodeToString(h[:8])
}

// Sign signs data with this device's private key.
func (id *DeviceIdentity) Sign(data []byte) ([]byte, error) {
	if id.privateKey == nil {
		return nil, fmt.Errorf("signing key not available")
	}
	hash := sha256.Sum256(data)
	r, s, err := ecdsa.Sign(rand.Reader, id.privateKey, hash[:])
	if err != nil {
		return nil, fmt.Errorf("signing failed: %w", err)
	}
	// Encode r||s as fixed-size 64 bytes.
	sig := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)
	return sig, nil
}

// Verify checks a signature against a public key hex.
func VerifySignature(pubKeyHex string, data, signature []byte) bool {
	pubBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil || len(signature) != 64 {
		return false
	}
	x, y := elliptic.UnmarshalCompressed(elliptic.P256(), pubBytes)
	if x == nil {
		return false
	}
	pubKey := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	hash := sha256.Sum256(data)
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	return ecdsa.Verify(pubKey, hash[:], r, s)
}

// NewDeviceManager creates a device manager with the given identity.
func NewDeviceManager(identity DeviceIdentity, maxPaired int) *DeviceManager {
	if maxPaired <= 0 {
		maxPaired = 10
	}
	return &DeviceManager{
		identity:      identity,
		pairedDevices: make(map[string]*PairedDevice),
		maxPaired:     maxPaired,
	}
}

// Identity returns the local device identity.
func (dm *DeviceManager) Identity() *DeviceIdentity {
	return &dm.identity
}

// InitiatePairing registers a remote device as pending.
func (dm *DeviceManager) InitiatePairing(remoteID, remotePubKey, remoteName string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if _, exists := dm.pairedDevices[remoteID]; exists {
		return fmt.Errorf("device %s already in pairing list", remoteID)
	}
	if len(dm.pairedDevices) >= dm.maxPaired {
		return fmt.Errorf("max paired devices (%d) reached", dm.maxPaired)
	}

	dm.pairedDevices[remoteID] = &PairedDevice{
		DeviceID:     remoteID,
		PublicKeyHex: remotePubKey,
		DeviceName:   remoteName,
		State:        PairingPending,
	}
	return nil
}

// VerifyPairing moves a pending device to verified.
func (dm *DeviceManager) VerifyPairing(remoteID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dev, ok := dm.pairedDevices[remoteID]
	if !ok {
		return fmt.Errorf("device %s not found", remoteID)
	}
	if dev.State != PairingPending {
		return fmt.Errorf("device %s not in pending state (is %s)", remoteID, dev.State)
	}

	now := time.Now()
	dev.State = PairingVerified
	dev.PairedAt = &now
	dev.LastSeen = &now
	return nil
}

// RejectPairing marks a device as rejected.
func (dm *DeviceManager) RejectPairing(remoteID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dev, ok := dm.pairedDevices[remoteID]
	if !ok {
		return fmt.Errorf("device %s not found", remoteID)
	}
	dev.State = PairingRejected
	return nil
}

// Unpair removes a device from the pairing list entirely.
func (dm *DeviceManager) Unpair(remoteID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if _, ok := dm.pairedDevices[remoteID]; !ok {
		return fmt.Errorf("device %s not found", remoteID)
	}
	delete(dm.pairedDevices, remoteID)
	return nil
}

// TrustedDevices returns all verified devices.
func (dm *DeviceManager) TrustedDevices() []*PairedDevice {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	var trusted []*PairedDevice
	for _, dev := range dm.pairedDevices {
		if dev.State == PairingVerified {
			trusted = append(trusted, dev)
		}
	}
	return trusted
}

// IsTrusted checks if a specific device is verified.
func (dm *DeviceManager) IsTrusted(remoteID string) bool {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	dev, ok := dm.pairedDevices[remoteID]
	return ok && dev.State == PairingVerified
}

// RecordSeen updates the last_seen timestamp for a device.
func (dm *DeviceManager) RecordSeen(remoteID string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dev, ok := dm.pairedDevices[remoteID]; ok {
		now := time.Now()
		dev.LastSeen = &now
	}
}

// PairedCount returns the total number of paired devices (all states).
func (dm *DeviceManager) PairedCount() int {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return len(dm.pairedDevices)
}

// randomHex generates n random bytes as hex.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
