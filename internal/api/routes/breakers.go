package routes

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/llm"
)

// GetBreakers returns the status of all circuit breakers.
func GetBreakers(svc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeJSON(w, http.StatusOK, map[string]any{"breakers": map[string]any{}})
			return
		}
		breakers := svc.Breakers()
		if breakers == nil {
			writeJSON(w, http.StatusOK, map[string]any{"breakers": map[string]any{}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"breakers": breakers.Status()})
	}
}

// ResetBreakers resets ALL circuit breakers, unblocking all providers.
func ResetBreakers(svc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "LLM service not available")
			return
		}
		breakers := svc.Breakers()
		if breakers == nil {
			writeError(w, http.StatusServiceUnavailable, "breaker registry not available")
			return
		}
		count := breakers.ResetAll()
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "reset",
			"count":  count,
		})
	}
}

// ResetBreakerProvider resets a single provider's circuit breaker.
func ResetBreakerProvider(svc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := chi.URLParam(r, "provider")
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "LLM service not available")
			return
		}
		breakers := svc.Breakers()
		if breakers == nil {
			writeError(w, http.StatusServiceUnavailable, "breaker registry not available")
			return
		}
		if !breakers.ResetProvider(provider) {
			writeError(w, http.StatusNotFound, "provider breaker not found: "+provider)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":   "reset",
			"provider": provider,
		})
	}
}
