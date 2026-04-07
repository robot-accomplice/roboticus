package routes

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// getOrCreateDeviceIdentity returns the persistent device identity, creating
// one on first call. It stores device_id, device_public_key, and
// device_private_key in the identity table.
func getOrCreateDeviceIdentity(r *http.Request, store *db.Store) (deviceID, publicKeyHex, fingerprint string, err error) {
	ctx := r.Context()

	// Try to load existing identity.
	var existing string
	row := store.QueryRowContext(ctx, `SELECT value FROM identity WHERE key = 'device_id'`)
	if row.Scan(&existing) == nil && existing != "" {
		deviceID = existing
		row2 := store.QueryRowContext(ctx, `SELECT value FROM identity WHERE key = 'device_public_key'`)
		if err2 := row2.Scan(&publicKeyHex); err2 != nil {
			return "", "", "", fmt.Errorf("device_id exists but public key missing: %w", err2)
		}
		pubBytes, decErr := hex.DecodeString(publicKeyHex)
		if decErr != nil {
			return "", "", "", fmt.Errorf("invalid public key hex: %w", decErr)
		}
		hash := sha256.Sum256(pubBytes)
		fingerprint = hex.EncodeToString(hash[:])
		return deviceID, publicKeyHex, fingerprint, nil
	}

	// Generate new identity.
	pub, priv, genErr := ed25519.GenerateKey(rand.Reader)
	if genErr != nil {
		return "", "", "", fmt.Errorf("failed to generate ed25519 keypair: %w", genErr)
	}

	hash := sha256.Sum256([]byte(pub))
	deviceID = fmt.Sprintf("dev-%s", hex.EncodeToString(hash[:8]))
	publicKeyHex = hex.EncodeToString([]byte(pub))
	privateKeyHex := hex.EncodeToString([]byte(priv))
	fingerprint = hex.EncodeToString(hash[:])

	// Persist all three values.
	for _, kv := range [][2]string{
		{"device_id", deviceID},
		{"device_public_key", publicKeyHex},
		{"device_private_key", privateKeyHex},
	} {
		_, err = store.ExecContext(ctx,
			`INSERT INTO identity (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			kv[0], kv[1])
		if err != nil {
			return "", "", "", fmt.Errorf("failed to persist identity key %q: %w", kv[0], err)
		}
	}

	return deviceID, publicKeyHex, fingerprint, nil
}

// GetRuntimeSurfaces returns the registered runtime surfaces (agent capabilities).
func GetRuntimeSurfaces() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		surfaces := []map[string]any{
			{"name": "chat", "description": "Multi-channel conversational interface"},
			{"name": "tool-use", "description": "Function calling and tool execution"},
			{"name": "memory", "description": "Five-tier memory architecture"},
			{"name": "scheduling", "description": "Cron-based task scheduling"},
			{"name": "delegation", "description": "Multi-agent delegation support"},
			{"name": "a2a", "description": "Agent-to-agent protocol"},
		}
		writeJSON(w, http.StatusOK, map[string]any{"surfaces": surfaces})
	}
}

// GetRuntimeDiscovery lists discovered agents from the database.
func GetRuntimeDiscovery(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, did, agent_card_json, capabilities, endpoint_url, trust_score, last_verified_at, created_at
			 FROM discovered_agents ORDER BY created_at DESC LIMIT 100`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query discovered agents")
			return
		}
		defer func() { _ = rows.Close() }()

		var agents []map[string]any
		for rows.Next() {
			var id, did, cardJSON, endpoint, createdAt string
			var capabilities, lastVerified *string
			var trustScore float64
			if err := rows.Scan(&id, &did, &cardJSON, &capabilities, &endpoint, &trustScore, &lastVerified, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read discovered agent row")
				return
			}
			agent := map[string]any{
				"id":           id,
				"did":          did,
				"endpoint_url": endpoint,
				"trust_score":  trustScore,
				"created_at":   createdAt,
			}
			if capabilities != nil {
				agent["capabilities"] = *capabilities
			}
			if lastVerified != nil {
				agent["last_verified_at"] = *lastVerified
			}
			// Parse agent card JSON.
			var card map[string]any
			if json.Unmarshal([]byte(cardJSON), &card) == nil {
				agent["agent_card"] = card
			}
			agents = append(agents, agent)
		}

		if agents == nil {
			agents = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
	}
}

// RegisterDiscoveredAgent registers a new discovered agent.
func RegisterDiscoveredAgent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DID          string  `json:"did"`
			AgentCard    any     `json:"agent_card"`
			Capabilities string  `json:"capabilities"`
			EndpointURL  string  `json:"endpoint_url"`
			TrustScore   float64 `json:"trust_score"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if body.DID == "" || body.EndpointURL == "" {
			writeError(w, http.StatusBadRequest, "did and endpoint_url are required")
			return
		}

		cardJSON, _ := json.Marshal(body.AgentCard)
		if body.TrustScore == 0 {
			body.TrustScore = 0.5
		}

		id := db.NewID()
		_, err := store.ExecContext(r.Context(),
			`INSERT INTO discovered_agents (id, did, agent_card_json, capabilities, endpoint_url, trust_score)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(did) DO UPDATE SET
			   agent_card_json = excluded.agent_card_json,
			   capabilities = excluded.capabilities,
			   endpoint_url = excluded.endpoint_url,
			   trust_score = excluded.trust_score`,
			id, body.DID, string(cardJSON), body.Capabilities, body.EndpointURL, body.TrustScore)
		if err != nil {
			log.Warn().Err(err).Msg("runtime: failed to register agent")
			writeError(w, http.StatusInternalServerError, "failed to register agent")
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "did": body.DID})
	}
}

// VerifyDiscoveredAgent marks a discovered agent as verified.
func VerifyDiscoveredAgent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		res, err := store.ExecContext(r.Context(),
			`UPDATE discovered_agents
			 SET last_verified_at = datetime('now')
			 WHERE id = ?`,
			id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify discovered agent")
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			writeError(w, http.StatusNotFound, "discovered agent not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	}
}

// GetRuntimeDevices lists paired devices.
func GetRuntimeDevices(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, device_name, state, paired_at, verified_at, last_seen
			 FROM paired_devices ORDER BY paired_at DESC`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query paired devices")
			return
		}
		defer func() { _ = rows.Close() }()

		devices := make([]map[string]any, 0)
		for rows.Next() {
			var id, deviceName, state, pairedAt, lastSeen string
			var verifiedAt *string
			if err := rows.Scan(&id, &deviceName, &state, &pairedAt, &verifiedAt, &lastSeen); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read paired device row")
				return
			}
			device := map[string]any{
				"id":          id,
				"device_id":   id,
				"device_name": deviceName,
				"state":       state,
				"paired_at":   pairedAt,
				"last_seen":   lastSeen,
			}
			if verifiedAt != nil {
				device["verified_at"] = *verifiedAt
			}
			devices = append(devices, device)
		}
		if devices == nil {
			devices = []map[string]any{}
		}
		identity := map[string]any{"device_id": "unknown"}
		devID, pubKey, fp, idErr := getOrCreateDeviceIdentity(r, store)
		if idErr != nil {
			log.Warn().Err(idErr).Msg("runtime: failed to resolve device identity")
		} else {
			identity["device_id"] = devID
			identity["public_key_hex"] = pubKey
			identity["fingerprint"] = fp
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"identity": identity,
			"devices":  devices,
		})
	}
}

// PairRuntimeDevice registers a device in pending state.
func PairRuntimeDevice(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DeviceID     string `json:"device_id"`
			PublicKeyHex string `json:"public_key_hex"`
			DeviceName   string `json:"device_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		body.DeviceID = strings.TrimSpace(body.DeviceID)
		body.DeviceName = strings.TrimSpace(body.DeviceName)
		body.PublicKeyHex = strings.TrimSpace(body.PublicKeyHex)
		if body.DeviceID == "" || body.PublicKeyHex == "" || body.DeviceName == "" {
			writeError(w, http.StatusBadRequest, "device_id, public_key_hex, and device_name are required")
			return
		}
		_, err := store.ExecContext(r.Context(),
			`INSERT INTO paired_devices (id, public_key_hex, device_name, state, paired_at, last_seen)
			 VALUES (?, ?, ?, 'pending', datetime('now'), datetime('now'))
			 ON CONFLICT(id) DO UPDATE SET
			   public_key_hex = excluded.public_key_hex,
			   device_name = excluded.device_name,
			   state = 'pending',
			   verified_at = NULL,
			   last_seen = datetime('now')`,
			body.DeviceID, body.PublicKeyHex, body.DeviceName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to pair device")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device_id": body.DeviceID})
	}
}

// VerifyPairedDevice marks a device as verified.
func VerifyPairedDevice(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		res, err := store.ExecContext(r.Context(),
			`UPDATE paired_devices
			 SET state = 'verified',
			     verified_at = datetime('now'),
			     last_seen = datetime('now')
			 WHERE id = ?`,
			id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify paired device")
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			writeError(w, http.StatusBadRequest, "paired device not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device_id": id})
	}
}

// UnpairDevice removes a paired device.
func UnpairDevice(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		res, err := store.ExecContext(r.Context(), `DELETE FROM paired_devices WHERE id = ?`, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to unpair device")
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			writeError(w, http.StatusNotFound, "paired device not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device_id": id})
	}
}
