package routes

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

// ListAgents returns the shared operator agent registry projection.
func ListAgents(store *db.Store, cfgs ...*core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cfg *core.Config
		if len(cfgs) > 0 {
			cfg = cfgs[0]
		}
		agents, err := buildAgentRegistryView(r.Context(), store, cfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query agents")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
	}
}

// StartAgent sets an agent's status to "running" by enabling it.
func StartAgent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		repo := db.NewAgentsRepository(store)
		if err := repo.SetEnabledByNameOrID(r.Context(), id, true); err != nil {
			if errors.Is(err, db.ErrNoRowsAffected) {
				writeError(w, http.StatusNotFound, "agent not found")
			} else {
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "running"})
	}
}

// StopAgent sets an agent's status to "stopped" by disabling it.
func StopAgent(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		repo := db.NewAgentsRepository(store)
		if err := repo.SetEnabledByNameOrID(r.Context(), id, false); err != nil {
			if errors.Is(err, db.ErrNoRowsAffected) {
				writeError(w, http.StatusNotFound, "agent not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		n := int64(1)
		if n == 0 {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "stopped"})
	}
}

// A2AHello returns the agent card for A2A discovery handshake.
func A2AHello() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		card := map[string]any{
			"name":        "roboticus",
			"description": "Autonomous AI agent runtime",
			"version":     "0.1.0",
			"protocol":    "a2a/1.0",
			"capabilities": []string{
				"chat", "tool-use", "multi-model", "memory",
				"multi-channel", "scheduling", "delegation",
			},
		}
		writeJSON(w, http.StatusOK, card)
	}
}
