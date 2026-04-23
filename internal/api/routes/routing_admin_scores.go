package routes

import (
	"fmt"
	"net/http"
	"strings"

	"roboticus/internal/llm"
)

// ResetModelScores clears metascore quality observations for one model or all models.
func ResetModelScores(llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		model := strings.TrimSpace(r.URL.Query().Get("model"))
		cleared := 0
		if llmSvc != nil {
			cleared = llmSvc.ResetQualityScores(model)
		}

		message := fmt.Sprintf("cleared %d observation entries for all models", cleared)
		modelValue := any(nil)
		if model != "" {
			modelValue = model
			message = fmt.Sprintf("cleared %d observation entries for %s", cleared, model)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"cleared": cleared,
			"model":   modelValue,
			"message": message,
		})
	}
}
