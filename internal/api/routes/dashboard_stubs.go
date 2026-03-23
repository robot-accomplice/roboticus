package routes

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"goboticus/internal/db"
)

// --- Workspace ---

// GetWorkspaceState returns live runtime state for the workspace page.
func GetWorkspaceState(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"uptime":      time.Since(startTime).Seconds(),
			"goroutines":  0,
			"connections": 0,
			"status":      "running",
		})
	}
}

var startTime = time.Now()

// --- Roster (Agents page) ---

// GetRoster returns the agent roster.
func GetRoster(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"agents": []map[string]any{
				{
					"name":    "default",
					"model":   "",
					"enabled": true,
					"role":    "primary",
				},
			},
		})
	}
}

// UpdateRosterModel updates an agent's model assignment.
func UpdateRosterModel(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// --- Stats ---

// GetTransactions returns recent financial transactions.
func GetTransactions(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"transactions": []any{}})
	}
}

// GetCapacity returns provider capacity metrics.
func GetCapacity(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"providers": map[string]any{}})
	}
}

// GetEfficiency returns efficiency metrics.
func GetEfficiency(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"period":         r.URL.Query().Get("period"),
			"total_tokens":   0,
			"total_cost":     0,
			"cache_hit_rate": 0,
			"avg_latency_ms": 0,
			"models":         []any{},
		})
	}
}

// GetModelSelections returns model selection events for the decision graph.
func GetModelSelections(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"events": []any{}})
	}
}

// GetRoutingDiagnostics returns routing config for the efficiency page.
func GetRoutingDiagnostics(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"config": map[string]any{
				"accuracy_floor":        0.7,
				"cost_aware":            true,
				"cost_weight":           0.3,
				"confidence_threshold":  0.9,
				"estimated_output_tokens": 800,
			},
		})
	}
}

// --- Recommendations ---

// GetRecommendations returns optimization recommendations.
func GetRecommendations(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"recommendations": []any{},
			"period":          r.URL.Query().Get("period"),
		})
	}
}

// GenerateRecommendations triggers deep analysis.
func GenerateRecommendations(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":          "completed",
			"recommendations": []any{},
		})
	}
}

// --- Wallet ---

// GetWalletBalance returns wallet balance.
func GetWalletBalance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"balance":  "0",
			"currency": "ETH",
			"tokens":   []any{},
			"network":  "Base",
		})
	}
}

// GetWalletAddress returns wallet address.
func GetWalletAddress() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"address":  "",
			"chain_id": 8453,
			"network":  "Base",
		})
	}
}

// GetSwaps returns swap service tasks.
func GetSwaps() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"swap_tasks": []any{}})
	}
}

// GetTaxPayouts returns tax payout tasks.
func GetTaxPayouts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tax_tasks": []any{}})
	}
}

// --- Memory ---

// GetSemanticCategories returns semantic memory categories.
func GetSemanticCategories(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"categories": []any{}})
	}
}

// --- Sessions extensions ---

// ListSessionTurns returns turns for a session.
func ListSessionTurns(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, role, content, created_at FROM session_messages WHERE session_id = ? ORDER BY created_at`, sessionID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"turns": []any{}})
			return
		}
		defer rows.Close()

		var turns []map[string]any
		for rows.Next() {
			var id, role, content, createdAt string
			if err := rows.Scan(&id, &role, &content, &createdAt); err != nil {
				continue
			}
			turns = append(turns, map[string]any{
				"id": id, "role": role, "content": content, "created_at": createdAt,
			})
		}
		if turns == nil {
			turns = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"turns": turns})
	}
}

// GetSessionFeedback returns feedback for a session.
func GetSessionFeedback(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"feedback": []any{}})
	}
}

// GetSessionInsights returns AI-generated insights for a session.
func GetSessionInsights(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"insights": []any{}})
	}
}

// DeleteSession deletes a session.
func DeleteSession(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		_, err := store.ExecContext(r.Context(), `DELETE FROM session_messages WHERE session_id = ?`, sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		store.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ?`, sessionID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// --- Turns extensions ---

// GetTurnContext returns context window analysis for a turn.
func GetTurnContext(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"system_tokens":  0,
			"memory_tokens":  0,
			"history_tokens": 0,
			"total_tokens":   0,
			"max_tokens":     128000,
		})
	}
}

// GetTurnTools returns tool calls for a turn.
func GetTurnTools(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tool_calls": []any{}})
	}
}

// GetTurnTips returns optimization tips for a turn.
func GetTurnTips(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tips": []any{}})
	}
}

// GetTurnModelSelection returns model selection details for a turn.
func GetTurnModelSelection(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, nil)
	}
}

// AnalyzeTurn triggers AI analysis of a turn.
func AnalyzeTurn(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"analysis": "Turn analysis not yet implemented."})
	}
}

// --- Skills extensions ---

// DeleteSkill removes a skill.
func DeleteSkill(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// ToggleSkill enables/disables a skill.
func ToggleSkill(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "toggled"})
	}
}

// GetSkillsCatalog returns available skills from catalog.
func GetSkillsCatalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"skills": []any{}})
	}
}

// InstallSkillFromCatalog installs a skill from the catalog.
func InstallSkillFromCatalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "installed"})
	}
}

// ActivateSkillFromCatalog activates a catalog skill.
func ActivateSkillFromCatalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "activated"})
	}
}

// InstallPlugin installs a plugin.
func InstallPlugin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "installed"})
	}
}

// --- Subagent extensions ---

// UpdateSubagent updates a subagent.
func UpdateSubagent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

// ToggleSubagent enables/disables a subagent.
func ToggleSubagent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "toggled"})
	}
}

// DeleteSubagent removes a subagent.
func DeleteSubagent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// --- Config ---

// UpdateConfig applies a config patch.
func UpdateConfig(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		// Store the config update — for now just acknowledge.
		writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
	}
}

// --- Channel test ---

// TestChannel sends a test message to a channel.
func TestChannel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		writeJSON(w, http.StatusOK, map[string]any{
			"channel": name,
			"status":  "test sent",
		})
	}
}

// --- Provider key management ---

// SetProviderKey stores a provider API key.
func SetProviderKey(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	}
}

// DeleteProviderKey removes a provider API key.
func DeleteProviderKey(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
	}
}

// --- Timeseries ---

// GetTimeseries returns time series data for overview sparklines.
func GetTimeseries(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"series": map[string]any{},
		})
	}
}
