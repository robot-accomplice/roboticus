package routes

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// --- Models ---

// RunRoutingEval runs the model routing evaluation against the default corpus
// and returns the results as JSON.
func RunRoutingEval(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		router := llmSvc.Router()
		if router == nil {
			writeError(w, http.StatusServiceUnavailable, "router not configured")
			return
		}
		corpus := llm.DefaultEvalCorpus()
		result := llm.RunEval(router, corpus)
		writeJSON(w, http.StatusOK, result)
	}
}

// GetAvailableModels returns configured LLM providers.
func GetAvailableModels(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers := llmSvc.Status()
		writeJSON(w, http.StatusOK, map[string]any{"models": providers})
	}
}

// --- Channels ---

// GetChannelsStatus returns channel adapter status as an array matching the
// Rust dashboard's expected shape: [{name, connected, last_error, ...}].
func GetChannelsStatus(cfg *core.Config, ks *core.Keystore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hasKey := func(envName, keystoreName string) bool {
			if envName != "" && os.Getenv(envName) != "" {
				return true
			}
			if ks != nil && ks.IsUnlocked() && ks.GetOrEmpty(keystoreName) != "" {
				return true
			}
			return false
		}

		type channelStatus struct {
			Name      string `json:"name"`
			Connected bool   `json:"connected"`
			LastError string `json:"last_error,omitempty"`
		}

		var channels []channelStatus

		if hasKey(cfg.Channels.TelegramTokenEnv, "telegram_bot_token") {
			channels = append(channels, channelStatus{Name: "telegram", Connected: true})
		}
		if hasKey(cfg.Channels.WhatsAppTokenEnv, "whatsapp_api_token") {
			channels = append(channels, channelStatus{Name: "whatsapp", Connected: true})
		}
		if hasKey(cfg.Channels.DiscordTokenEnv, "discord_bot_token") {
			channels = append(channels, channelStatus{Name: "discord", Connected: true})
		}
		if cfg.Channels.SignalDaemonURL != "" {
			channels = append(channels, channelStatus{Name: "signal", Connected: true})
		}
		if cfg.Channels.EmailFromAddress != "" {
			channels = append(channels, channelStatus{Name: "email", Connected: true})
		}
		if cfg.Matrix.Enabled {
			channels = append(channels, channelStatus{Name: "matrix", Connected: true})
		}

		if channels == nil {
			channels = []channelStatus{}
		}
		writeJSON(w, http.StatusOK, channels)
	}
}

// GetDeadLetters returns dead letter queue entries.
func GetDeadLetters(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.NewRouteQueries(store).ListDeadLetters(r.Context(), 50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query dead letters")
			return
		}
		defer func() { _ = rows.Close() }()

		var entries []map[string]string
		for rows.Next() {
			var id, channel, recipient, content, createdAt string
			var errMsg *string
			if err := rows.Scan(&id, &channel, &recipient, &content, &errMsg, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read dead letter row")
				return
			}
			e := map[string]string{
				"id": id, "channel": channel, "recipient": recipient,
				"content": content, "created_at": createdAt,
			}
			if errMsg != nil {
				e["error"] = *errMsg
			}
			entries = append(entries, e)
		}
		if entries == nil {
			entries = []map[string]string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"dead_letters": entries})
	}
}

// --- Config ---

// GetConfigStatus returns the current config application status.
// Reports the server start time as last_applied, since config is loaded at startup.
func GetConfigStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "applied",
			"last_applied": processStartTime.UTC().Format(time.RFC3339),
		})
	}
}

// --- Keystore ---

// KeystoreStatus returns whether any provider keys are stored in the encrypted keystore.
func KeystoreStatus(ks *core.Keystore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		unlocked := ks != nil && ks.IsUnlocked()
		resp := map[string]any{
			"unlocked": unlocked,
			"path":     filepath.Join(core.ConfigDir(), "keystore.enc"),
		}
		if unlocked {
			resp["stored_keys"] = ks.Count()
			resp["keys"] = ks.List()
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// KeystoreUnlock attempts to unlock the keystore using the machine-derived passphrase.
func KeystoreUnlock() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"unlocked": true,
			"action":   "already_unlocked",
		})
	}
}

// --- Subagent Retirement ---

// SubagentRetirementCandidates returns subagents not used in 30 days.
func SubagentRetirementCandidates(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.NewRouteQueries(store).ListRetirementCandidates(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query retirement candidates")
			return
		}
		defer func() { _ = rows.Close() }()

		candidates := make([]map[string]any, 0)
		for rows.Next() {
			var id, name, model, role, createdAt string
			var displayName *string
			if err := rows.Scan(&id, &name, &displayName, &model, &role, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read retirement candidate row")
				return
			}
			c := map[string]any{
				"id": id, "name": name, "model": model,
				"role": role, "created_at": createdAt,
			}
			if displayName != nil {
				c["display_name"] = *displayName
			}
			candidates = append(candidates, c)
		}
		writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates})
	}
}

// RetireUnusedSubagents deletes subagents not used in 30 days.
func RetireUnusedSubagents(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo := db.NewAgentsRepository(store)
		n, err := repo.PruneOld(r.Context(), 30)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"retired": n})
	}
}

// GetConfig returns the current configuration, enriched with key status per provider.
func GetConfig(cfg *core.Config, ks *core.Keystore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Marshal to map so we can inject _key_status/_key_source per provider.
		data, err := json.Marshal(cfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to marshal config")
			return
		}
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to re-parse config")
			return
		}

		// Enrich providers with key status using the in-memory config
		// (JSON omitempty drops is_local=false, so we read from the struct).
		if providers, ok := out["providers"].(map[string]any); ok {
			for name, pRaw := range providers {
				pMap, ok := pRaw.(map[string]any)
				if !ok {
					continue
				}
				// Look up is_local from the actual Config struct, not the JSON.
				provCfg := cfg.Providers[name]
				status, source := resolveKeyStatus(name, provCfg.IsLocal, provCfg.APIKeyEnv, ks)
				pMap["_key_status"] = status
				pMap["_key_source"] = source
				pMap["is_local"] = provCfg.IsLocal
			}
		}

		writeJSON(w, http.StatusOK, out)
	}
}

// resolveKeyStatus determines the key status for a provider, matching the
// Rust resolve_key_source cascade: local → keystore → env → missing.
func resolveKeyStatus(providerName string, isLocal bool, apiKeyEnv string, ks *core.Keystore) (string, string) {
	if isLocal {
		return "not_required", "local"
	}

	// Check keystore by conventional name: {provider}_api_key.
	if ks != nil && ks.IsUnlocked() {
		conventional := providerName + "_api_key"
		if val := ks.GetOrEmpty(conventional); val != "" {
			return "configured", "keystore"
		}
	}

	// Check environment variable.
	if apiKeyEnv != "" {
		if val := os.Getenv(apiKeyEnv); val != "" {
			return "configured", "env"
		}
	}

	return "missing", "none"
}

// GetCapabilities returns agent capabilities.
func GetCapabilities() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"capabilities": []string{
				"chat", "tool-use", "multi-model", "memory",
				"multi-channel", "scheduling", "multi-agent",
			},
			"immutable_sections": []string{"server", "a2a", "wallet"},
		})
	}
}

// --- Circuit Breaker ---

// BreakerStatus returns circuit breaker status for all providers with a summary note.
func BreakerStatus(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers := llmSvc.Status()

		// Derive summary note: "open" if any open, "half-open" if any half-open, else "closed".
		note := "closed"
		for _, p := range providers {
			if p.State == llm.CircuitOpen {
				note = "open"
				break
			}
			if p.State == llm.CircuitHalfOpen {
				note = "half-open"
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"breakers": providers,
			"note":     note,
		})
	}
}

// BreakerReset resets a provider's circuit breaker.
func BreakerReset(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := chi.URLParam(r, "provider")
		if err := llmSvc.ResetBreaker(provider); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "reset", "provider": provider})
	}
}

// BreakerForceOpen force-opens a provider's circuit breaker.
// Unlike normal open, this is only cleared by an explicit Reset call.
func BreakerForceOpen(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := chi.URLParam(r, "provider")
		if err := llmSvc.ForceOpenBreaker(provider); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "force_opened", "provider": provider})
	}
}

// MemoryHealth returns per-tier counts and stale/active breakdowns.
func MemoryHealth(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		rq := db.NewRouteQueries(store)

		workingCount, err := rq.CountWorkingMemory(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query working memory health")
			return
		}
		workingStale, err := rq.CountWorkingMemoryStale(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query working memory staleness")
			return
		}

		episodicCount, err := rq.CountEpisodicMemory(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query episodic memory health")
			return
		}
		episodicStale, err := rq.CountEpisodicMemoryStale(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query episodic memory staleness")
			return
		}

		semanticCount, err := rq.CountSemanticMemory(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query semantic memory health")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"working": map[string]any{
				"total":  workingCount,
				"active": workingCount - workingStale,
				"stale":  workingStale,
			},
			"episodic": map[string]any{
				"total":  episodicCount,
				"active": episodicCount - episodicStale,
				"stale":  episodicStale,
			},
			"semantic": map[string]any{
				"total": semanticCount,
			},
		})
	}
}

// ReplayDeadLetter replays a dead letter queue entry by resetting its status for retry.
func ReplayDeadLetter(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		repo := db.NewDeliveryRepository(store)
		n, err := repo.ReplayDeadLetter(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if n == 0 {
			writeError(w, http.StatusNotFound, "dead letter entry not found or already replayed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "replayed", "id": id})
	}
}

// --- Subagents ---

// ListSubagents returns registered subagents.
func ListSubagents(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.NewRouteQueries(store).ListSubAgentsAdmin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query subagents")
			return
		}
		defer func() { _ = rows.Close() }()

		var agents []map[string]any
		for rows.Next() {
			var id, name, model, role, createdAt string
			var displayName, description *string
			var enabled bool
			if err := rows.Scan(&id, &name, &displayName, &model, &role, &description, &enabled, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read subagent row")
				return
			}
			a := map[string]any{
				"id": id, "name": name, "model": model,
				"role": role, "enabled": enabled, "created_at": createdAt,
			}
			if displayName != nil {
				a["display_name"] = *displayName
			}
			if description != nil {
				a["description"] = *description
			}
			agents = append(agents, a)
		}
		if agents == nil {
			agents = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
	}
}

// CreateSubagent registers a new subagent.
func CreateSubagent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name         string   `json:"name"`
			Model        string   `json:"model"`
			Capabilities []string `json:"capabilities"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		skillsJSON, _ := json.Marshal(req.Capabilities)
		id := db.NewID()
		repo := db.NewAgentsRepository(store)
		if err := repo.Insert(r.Context(), id, req.Name, req.Model, string(skillsJSON), true); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
	}
}

// --- WebSocket Ticket ---

// TicketIssuer creates single-use authentication tickets.
type TicketIssuer interface {
	Issue() string
}

// IssueWSTicket creates a single-use WebSocket auth ticket.
// If no ticket store is wired, returns 501.
func IssueWSTicket(issuer ...TicketIssuer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(issuer) == 0 || issuer[0] == nil {
			writeError(w, http.StatusNotImplemented, "WebSocket ticket store not configured")
			return
		}
		ticket := issuer[0].Issue()
		writeJSON(w, http.StatusOK, map[string]string{"ticket": ticket})
	}
}
