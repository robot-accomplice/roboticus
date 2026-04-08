package db

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// RuntimeRepository handles runtime identity and device persistence.
type RuntimeRepository struct {
	q Querier
}

// NewRuntimeRepository creates a runtime repository.
func NewRuntimeRepository(q Querier) *RuntimeRepository {
	return &RuntimeRepository{q: q}
}

// GetOrCreateDeviceIdentity returns the persistent device identity, creating
// a new ed25519 keypair on first call. Returns (deviceID, publicKeyHex, fingerprint).
func (r *RuntimeRepository) GetOrCreateDeviceIdentity(ctx context.Context) (string, string, string, error) {
	// Try to load existing identity.
	var existing string
	row := r.q.QueryRowContext(ctx, `SELECT value FROM identity WHERE key = 'device_id'`)
	if row.Scan(&existing) == nil && existing != "" {
		var publicKeyHex string
		row2 := r.q.QueryRowContext(ctx, `SELECT value FROM identity WHERE key = 'device_public_key'`)
		if err := row2.Scan(&publicKeyHex); err != nil {
			return "", "", "", fmt.Errorf("device_id exists but public key missing: %w", err)
		}
		pubBytes, decErr := hex.DecodeString(publicKeyHex)
		if decErr != nil {
			return "", "", "", fmt.Errorf("invalid public key hex: %w", decErr)
		}
		hash := sha256.Sum256(pubBytes)
		return existing, publicKeyHex, hex.EncodeToString(hash[:]), nil
	}

	// Generate new identity.
	pub, priv, genErr := ed25519.GenerateKey(rand.Reader)
	if genErr != nil {
		return "", "", "", fmt.Errorf("generate ed25519 keypair: %w", genErr)
	}

	hash := sha256.Sum256([]byte(pub))
	deviceID := fmt.Sprintf("dev-%s", hex.EncodeToString(hash[:8]))
	publicKeyHex := hex.EncodeToString([]byte(pub))
	privateKeyHex := hex.EncodeToString([]byte(priv))
	fingerprint := hex.EncodeToString(hash[:])

	for _, kv := range [][2]string{
		{"device_id", deviceID},
		{"device_public_key", publicKeyHex},
		{"device_private_key", privateKeyHex},
	} {
		if err := r.SetIdentity(ctx, kv[0], kv[1]); err != nil {
			return "", "", "", fmt.Errorf("persist identity key %q: %w", kv[0], err)
		}
	}

	return deviceID, publicKeyHex, fingerprint, nil
}

// SetIdentity upserts a key-value pair in the identity table.
func (r *RuntimeRepository) SetIdentity(ctx context.Context, key, value string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO identity (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// PairDevice registers or updates a paired device with pending state.
func (r *RuntimeRepository) PairDevice(ctx context.Context, id, publicKeyHex, deviceName string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO paired_devices (id, public_key_hex, device_name, state, paired_at, last_seen)
		 VALUES (?, ?, ?, 'pending', datetime('now'), datetime('now'))
		 ON CONFLICT(id) DO UPDATE SET
		   public_key_hex = excluded.public_key_hex,
		   device_name = excluded.device_name,
		   state = 'pending',
		   verified_at = NULL,
		   last_seen = datetime('now')`,
		id, publicKeyHex, deviceName)
	return err
}

// VerifyPairedDevice marks a paired device as verified.
func (r *RuntimeRepository) VerifyPairedDevice(ctx context.Context, id string) (int64, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE paired_devices
		 SET state = 'verified',
		     verified_at = datetime('now'),
		     last_seen = datetime('now')
		 WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UnpairDevice removes a paired device by ID.
func (r *RuntimeRepository) UnpairDevice(ctx context.Context, id string) (int64, error) {
	res, err := r.q.ExecContext(ctx, `DELETE FROM paired_devices WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpsertDiscoveredAgent registers or updates a discovered agent.
func (r *RuntimeRepository) UpsertDiscoveredAgent(ctx context.Context, id, did, agentCardJSON, capabilities, endpointURL string, trustScore float64) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO discovered_agents (id, did, agent_card_json, capabilities, endpoint_url, trust_score)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(did) DO UPDATE SET
		   agent_card_json = excluded.agent_card_json,
		   capabilities = excluded.capabilities,
		   endpoint_url = excluded.endpoint_url,
		   trust_score = excluded.trust_score`,
		id, did, agentCardJSON, capabilities, endpointURL, trustScore)
	return err
}

// VerifyDiscoveredAgent marks a discovered agent as verified.
func (r *RuntimeRepository) VerifyDiscoveredAgent(ctx context.Context, id string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE discovered_agents SET last_verified_at = datetime('now') WHERE id = ?`, id)
	return err
}
