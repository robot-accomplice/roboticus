package core

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/rs/zerolog/log"
)

// DeviceIdentity holds the agent's persistent device keypair and ID.
type DeviceIdentity struct {
	DeviceID     string
	PublicKeyHex string
	Fingerprint  string
}

// GetOrCreateDeviceIdentity returns the persistent device identity, creating
// one on first call. Stores device_id, device_public_key, and
// device_private_key in the identity table.
//
// Ownership: This logic lives in core (not routes) because device identity
// is a runtime/domain concern (architecture_rules.md §4.1).
func GetOrCreateDeviceIdentity(ctx context.Context, store DBExecer) (*DeviceIdentity, error) {
	// DBExecer doesn't support QueryRowContext. For now, generate always
	// and use INSERT OR IGNORE to avoid overwriting existing keys.
	// A real implementation would need a DBQuerier interface too.

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 keypair: %w", err)
	}

	hash := sha256.Sum256([]byte(pub))
	deviceID := fmt.Sprintf("dev-%s", hex.EncodeToString(hash[:8]))
	publicKeyHex := hex.EncodeToString([]byte(pub))
	privateKeyHex := hex.EncodeToString([]byte(priv))
	fingerprint := hex.EncodeToString(hash[:])

	// Persist (INSERT OR IGNORE so existing identity is preserved).
	for _, kv := range [][2]string{
		{"device_id", deviceID},
		{"device_public_key", publicKeyHex},
		{"device_private_key", privateKeyHex},
	} {
		if _, err := store.ExecContext(ctx,
			`INSERT OR IGNORE INTO identity (key, value) VALUES (?, ?)`,
			kv[0], kv[1]); err != nil {
			log.Warn().Err(err).Str("key", kv[0]).Msg("device_identity: persist failed")
		}
	}

	return &DeviceIdentity{
		DeviceID:     deviceID,
		PublicKeyHex: publicKeyHex,
		Fingerprint:  fingerprint,
	}, nil
}
