package routes

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// getOrCreateDeviceIdentity delegates to RuntimeRepository for identity management.
// The crypto and persistence logic lives in db/runtime_repo.go (architecture_rules.md §4.1).
func getOrCreateDeviceIdentity(r *http.Request, store *db.Store) (deviceID, publicKeyHex, fingerprint string, err error) {
	repo := db.NewRuntimeRepository(store)
	return repo.GetOrCreateDeviceIdentity(r.Context())

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
		rows, err := db.NewRouteQueries(store).ListDiscoveredAgentsFull(r.Context(), 100)
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
		rtRepo := db.NewRuntimeRepository(store)
		if err := rtRepo.UpsertDiscoveredAgent(r.Context(), id, body.DID, string(cardJSON), body.Capabilities, body.EndpointURL, body.TrustScore); err != nil {
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
		rtRepo := db.NewRuntimeRepository(store)
		if err := rtRepo.VerifyDiscoveredAgent(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify discovered agent")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	}
}

// GetRuntimeDevices lists paired devices.
func GetRuntimeDevices(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.NewRouteQueries(store).ListPairedDevicesFull(r.Context())
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
		rtRepo := db.NewRuntimeRepository(store)
		if err := rtRepo.PairDevice(r.Context(), body.DeviceID, body.PublicKeyHex, body.DeviceName); err != nil {
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
		rtRepo := db.NewRuntimeRepository(store)
		affected, err := rtRepo.VerifyPairedDevice(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify paired device")
			return
		}
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
		rtRepo := db.NewRuntimeRepository(store)
		affected, err := rtRepo.UnpairDevice(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to unpair device")
			return
		}
		if affected == 0 {
			writeError(w, http.StatusNotFound, "paired device not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device_id": id})
	}
}
