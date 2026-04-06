package routes

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"

	"goboticus/internal/db"
)

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

// GetRuntimeDevices lists paired devices (currently returns empty list).
func GetRuntimeDevices() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"devices": []any{}})
	}
}
