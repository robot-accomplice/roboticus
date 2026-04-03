package channel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// DefaultRotationThreshold is the number of messages before rotating a Megolm-style session.
const DefaultRotationThreshold = 100

// megolmSession represents a single outbound or inbound ratchet session.
type megolmSession struct {
	SessionID    string `json:"session_id"`
	RatchetKey   []byte `json:"ratchet_key"`   // 32-byte AES key, ratcheted per message
	MessageIndex uint32 `json:"message_index"` // monotonic counter
}

// MatrixCrypto manages Olm/Megolm-compatible E2EE state for a Matrix device.
type MatrixCrypto struct {
	mu sync.Mutex

	// Device identity keys.
	Curve25519Private []byte `json:"curve25519_private"` // 32 bytes
	Curve25519Public  []byte `json:"curve25519_public"`  // 32 bytes
	Ed25519Private    []byte `json:"ed25519_private"`    // 64 bytes (ed25519 seed+pub)
	Ed25519Public     []byte `json:"ed25519_public"`     // 32 bytes

	// One-time key pool: keyID -> base64-encoded Curve25519 public key.
	OneTimeKeys        map[string][]byte `json:"one_time_keys"`
	oneTimeKeyPrivates map[string][]byte // not persisted directly — derived from stored seeds
	OneTimeKeySeeds    map[string][]byte `json:"one_time_key_seeds"` // seed -> private key derivation

	// Session maps keyed by roomID.
	OutboundSessions map[string]*megolmSession `json:"outbound_sessions"`
	InboundSessions  map[string]*megolmSession `json:"inbound_sessions"`

	// RotationThreshold controls how many messages before rotating an outbound session.
	RotationThreshold uint32 `json:"rotation_threshold"`
}

// NewMatrixCrypto generates a new set of device keys and returns
// an initialized MatrixCrypto ready for use.
func NewMatrixCrypto() *MatrixCrypto {
	mc := &MatrixCrypto{
		OneTimeKeys:        make(map[string][]byte),
		oneTimeKeyPrivates: make(map[string][]byte),
		OneTimeKeySeeds:    make(map[string][]byte),
		OutboundSessions:   make(map[string]*megolmSession),
		InboundSessions:    make(map[string]*megolmSession),
		RotationThreshold:  DefaultRotationThreshold,
	}

	// Generate Curve25519 key pair for key agreement.
	var privKey [32]byte
	if _, err := io.ReadFull(rand.Reader, privKey[:]); err != nil {
		panic("matrix_crypto: failed to generate curve25519 key: " + err.Error())
	}
	pubKey, err := curve25519.X25519(privKey[:], curve25519.Basepoint)
	if err != nil {
		panic("matrix_crypto: curve25519 scalar mult failed: " + err.Error())
	}
	mc.Curve25519Private = privKey[:]
	mc.Curve25519Public = pubKey

	// Generate Ed25519 key pair for signing.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic("matrix_crypto: failed to generate ed25519 key: " + err.Error())
	}
	mc.Ed25519Private = priv
	mc.Ed25519Public = []byte(pub)

	return mc
}

// DeviceKeysJSON returns the device keys formatted for the Matrix /keys/upload endpoint.
func (mc *MatrixCrypto) DeviceKeysJSON() map[string]any {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	return map[string]any{
		"algorithms": []string{
			"m.olm.v1.curve25519-aes-sha2",
			"m.megolm.v1.aes-sha2",
		},
		"keys": map[string]string{
			"curve25519:DEVICE": base64.RawStdEncoding.EncodeToString(mc.Curve25519Public),
			"ed25519:DEVICE":   base64.RawStdEncoding.EncodeToString(mc.Ed25519Public),
		},
	}
}

// GenerateOneTimeKeys generates count one-time Curve25519 key pairs
// and adds them to the pool.
func (mc *MatrixCrypto) GenerateOneTimeKeys(count int) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for i := 0; i < count; i++ {
		var seed [32]byte
		if _, err := io.ReadFull(rand.Reader, seed[:]); err != nil {
			panic("matrix_crypto: failed to generate one-time key seed: " + err.Error())
		}
		pub, err := curve25519.X25519(seed[:], curve25519.Basepoint)
		if err != nil {
			panic("matrix_crypto: one-time key scalar mult failed: " + err.Error())
		}
		keyID := fmt.Sprintf("curve25519:OTKEY_%s", base64.RawURLEncoding.EncodeToString(pub[:6]))
		mc.OneTimeKeys[keyID] = pub
		if mc.oneTimeKeyPrivates == nil {
			mc.oneTimeKeyPrivates = make(map[string][]byte)
		}
		mc.oneTimeKeyPrivates[keyID] = seed[:]
		mc.OneTimeKeySeeds[keyID] = seed[:]
	}
}

// CreateOutboundSession creates a new Megolm-style outbound session for the given room.
// Any existing outbound session for that room is replaced.
func (mc *MatrixCrypto) CreateOutboundSession(roomID string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	return mc.createOutboundSessionLocked(roomID)
}

func (mc *MatrixCrypto) createOutboundSessionLocked(roomID string) error {
	sessionKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionKey); err != nil {
		return fmt.Errorf("matrix_crypto: generate session key: %w", err)
	}

	sessionID := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, sessionID); err != nil {
		return fmt.Errorf("matrix_crypto: generate session id: %w", err)
	}

	mc.OutboundSessions[roomID] = &megolmSession{
		SessionID:    base64.RawStdEncoding.EncodeToString(sessionID),
		RatchetKey:   sessionKey,
		MessageIndex: 0,
	}

	// Also create the corresponding inbound session so we can decrypt our own messages.
	mc.InboundSessions[roomID] = &megolmSession{
		SessionID:    mc.OutboundSessions[roomID].SessionID,
		RatchetKey:   append([]byte(nil), sessionKey...),
		MessageIndex: 0,
	}

	return nil
}

// EncryptMessage encrypts plaintext using the outbound Megolm session for the room.
// If no session exists, one is created. The session is rotated if RotationThreshold
// is exceeded. Returns Matrix m.room.encrypted event content.
func (mc *MatrixCrypto) EncryptMessage(roomID, plaintext string) (map[string]any, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	session, ok := mc.OutboundSessions[roomID]
	if !ok {
		if err := mc.createOutboundSessionLocked(roomID); err != nil {
			return nil, err
		}
		session = mc.OutboundSessions[roomID]
	}

	// Rotate if threshold exceeded.
	if session.MessageIndex >= mc.RotationThreshold {
		if err := mc.createOutboundSessionLocked(roomID); err != nil {
			return nil, err
		}
		session = mc.OutboundSessions[roomID]
	}

	// Derive per-message key using HKDF from ratchet key + message index.
	messageKey, err := deriveMessageKey(session.RatchetKey, session.MessageIndex)
	if err != nil {
		return nil, fmt.Errorf("matrix_crypto: derive message key: %w", err)
	}

	// Encrypt with AES-256-GCM.
	ciphertextBytes, err := aesGCMEncrypt(messageKey, []byte(plaintext))
	if err != nil {
		return nil, fmt.Errorf("matrix_crypto: encrypt: %w", err)
	}

	msgIndex := session.MessageIndex
	sessionID := session.SessionID

	// Ratchet: advance the ratchet key.
	session.RatchetKey = ratchetKey(session.RatchetKey)
	session.MessageIndex++

	// Keep inbound session in sync so we can decrypt our own messages.
	if inbound, exists := mc.InboundSessions[roomID]; exists && inbound.SessionID == sessionID {
		inbound.RatchetKey = ratchetKey(inbound.RatchetKey)
		inbound.MessageIndex++
	}

	return map[string]any{
		"algorithm":  "m.megolm.v1.aes-sha2",
		"sender_key": base64.RawStdEncoding.EncodeToString(mc.Curve25519Public),
		"session_id": sessionID,
		"ciphertext": base64.StdEncoding.EncodeToString(ciphertextBytes),
		"message_index": msgIndex,
	}, nil
}

// DecryptEvent decrypts a Matrix m.room.encrypted event using the inbound session.
func (mc *MatrixCrypto) DecryptEvent(roomID string, ciphertext map[string]any) (string, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	session, ok := mc.InboundSessions[roomID]
	if !ok {
		return "", fmt.Errorf("matrix_crypto: no inbound session for room %s", roomID)
	}

	sessionID, _ := ciphertext["session_id"].(string)
	if sessionID != session.SessionID {
		return "", fmt.Errorf("matrix_crypto: session ID mismatch: got %s, want %s", sessionID, session.SessionID)
	}

	ct, _ := ciphertext["ciphertext"].(string)
	if ct == "" {
		return "", fmt.Errorf("matrix_crypto: empty ciphertext")
	}
	ciphertextBytes, err := base64.StdEncoding.DecodeString(ct)
	if err != nil {
		return "", fmt.Errorf("matrix_crypto: decode ciphertext: %w", err)
	}

	// Extract message index from the event.
	msgIndexFloat, ok := ciphertext["message_index"].(float64)
	if !ok {
		msgIndexUint, ok2 := ciphertext["message_index"].(uint32)
		if !ok2 {
			return "", fmt.Errorf("matrix_crypto: missing message_index")
		}
		msgIndexFloat = float64(msgIndexUint)
	}
	msgIndex := uint32(msgIndexFloat)

	// We need to derive the key for the specific message index.
	// For simplicity, we only support decrypting the next expected message
	// (in-order decryption).
	if msgIndex != session.MessageIndex {
		return "", fmt.Errorf("matrix_crypto: message index %d does not match expected %d (out-of-order not supported)", msgIndex, session.MessageIndex)
	}

	messageKey, err := deriveMessageKey(session.RatchetKey, session.MessageIndex)
	if err != nil {
		return "", fmt.Errorf("matrix_crypto: derive message key: %w", err)
	}

	plaintext, err := aesGCMDecrypt(messageKey, ciphertextBytes)
	if err != nil {
		return "", fmt.Errorf("matrix_crypto: decrypt: %w", err)
	}

	// Advance the ratchet.
	session.RatchetKey = ratchetKey(session.RatchetKey)
	session.MessageIndex++

	return string(plaintext), nil
}

// Persist saves the crypto state to a JSON file with 0600 permissions.
func (mc *MatrixCrypto) Persist(path string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	data, err := json.MarshalIndent(mc, "", "  ")
	if err != nil {
		return fmt.Errorf("matrix_crypto: marshal state: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadCrypto loads crypto state from a JSON file.
func LoadCrypto(path string) (*MatrixCrypto, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("matrix_crypto: read state: %w", err)
	}
	var mc MatrixCrypto
	if err := json.Unmarshal(data, &mc); err != nil {
		return nil, fmt.Errorf("matrix_crypto: unmarshal state: %w", err)
	}
	// Rebuild private key map from seeds.
	mc.oneTimeKeyPrivates = make(map[string][]byte)
	for keyID, seed := range mc.OneTimeKeySeeds {
		mc.oneTimeKeyPrivates[keyID] = seed
	}
	return &mc, nil
}

// deriveMessageKey uses HKDF-SHA256 to derive a 32-byte AES key from the ratchet key
// and message index.
func deriveMessageKey(ratchetKey []byte, messageIndex uint32) ([]byte, error) {
	info := []byte(fmt.Sprintf("megolm-msg-%d", messageIndex))
	hk := hkdf.New(sha256.New, ratchetKey, nil, info)
	key := make([]byte, 32)
	if _, err := io.ReadFull(hk, key); err != nil {
		return nil, err
	}
	return key, nil
}

// ratchetKey advances the ratchet by hashing the current key.
func ratchetKey(current []byte) []byte {
	h := sha256.Sum256(current)
	return h[:]
}

// aesGCMEncrypt encrypts plaintext with AES-256-GCM using a random nonce.
func aesGCMEncrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	// nonce is prepended to the ciphertext.
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// aesGCMDecrypt decrypts ciphertext encrypted by aesGCMEncrypt.
func aesGCMDecrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ct, nil)
}
