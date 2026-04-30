package routes

import (
	"encoding/json"
	"net/http"
	"strings"

	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// RescoreExerciseResults recomputes persisted exercise quality/pass values
// using the current scoring regime. It exists so benchmark-rubric changes do
// not force blind reruns when raw prompt/response artifacts are already stored.
func RescoreExerciseResults(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Models []string `json:"models,omitempty"`
			RunID  string   `json:"run_id,omitempty"`
			DryRun bool     `json:"dry_run,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		rows := db.ListExerciseResultsForRescore(r.Context(), store, req.Models, strings.TrimSpace(req.RunID))
		if rows == nil {
			rows = []db.ExerciseResultRow{}
		}

		updated := 0
		passFlips := 0
		qualityChanged := 0
		var previews []map[string]any

		for _, row := range rows {
			intent, err := llm.ParseIntentClassStrict(row.IntentClass)
			if err != nil {
				continue
			}
			complexity, err := llm.ParseComplexityLevel(row.Complexity)
			if err != nil {
				continue
			}
			prompt := llm.ResolveExercisePrompt(row.Prompt, intent, complexity)
			newClass := normalizeExerciseResultClass(row)
			newQuality := 0.0
			newPassed := false
			if exerciseResultClassCountsAsEfficacy(newClass) {
				newQuality = llm.ScoreExerciseResponse(prompt, row.Content)
				newPassed = newQuality >= llm.DefaultExercisePassQualityFloor && row.ErrorMsg == ""
				if row.ErrorMsg == "" && strings.TrimSpace(row.Content) != "" {
					if newPassed {
						newClass = string(llm.ExerciseOutcomeCleanPass)
					} else {
						newClass = string(llm.ExerciseOutcomeQualityGateFailure)
					}
				}
			}

			if newQuality != row.Quality {
				qualityChanged++
			}
			if newPassed != row.Passed {
				passFlips++
			}
			if !req.DryRun && (newQuality != row.Quality || newPassed != row.Passed || newClass != row.ResultClass) {
				if err := db.UpdateExerciseResultScore(r.Context(), store, row.ID, newQuality, newPassed, newClass); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to update rescored exercise result")
					return
				}
				updated++
			}

			if len(previews) < 10 && (newQuality != row.Quality || newPassed != row.Passed || newClass != row.ResultClass) {
				previews = append(previews, map[string]any{
					"id":          row.ID,
					"run_id":      row.RunID,
					"model":       row.Model,
					"prompt":      row.Prompt,
					"old_quality": row.Quality,
					"new_quality": newQuality,
					"old_passed":  row.Passed,
					"new_passed":  newPassed,
					"old_class":   row.ResultClass,
					"new_class":   newClass,
				})
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"total_rows":      len(rows),
			"quality_changed": qualityChanged,
			"pass_flips":      passFlips,
			"updated":         updated,
			"dry_run":         req.DryRun,
			"preview_changes": previews,
			"scoring_regime":  "prompt_contract_v1",
			"filtered_models": req.Models,
			"filtered_run_id": strings.TrimSpace(req.RunID),
		})
	}
}

func normalizeExerciseResultClass(row db.ExerciseResultRow) string {
	class := strings.TrimSpace(row.ResultClass)
	if class != "" {
		return class
	}
	if strings.TrimSpace(row.ErrorMsg) != "" {
		msg := strings.ToLower(row.ErrorMsg)
		if strings.Contains(msg, "timeout") ||
			strings.Contains(msg, "deadline exceeded") ||
			strings.Contains(msg, "client.timeout") ||
			strings.Contains(msg, "awaiting headers") {
			return string(llm.ExerciseOutcomeProviderTimeout)
		}
		return string(llm.ExerciseOutcomeTransportError)
	}
	if legacyBlankZeroFailure(row) {
		return string(llm.ExerciseOutcomeValidityAmbiguous)
	}
	if strings.TrimSpace(row.Content) == "" {
		return string(llm.ExerciseOutcomeEmptyResponse)
	}
	if row.Passed {
		return string(llm.ExerciseOutcomeCleanPass)
	}
	return string(llm.ExerciseOutcomeQualityGateFailure)
}

func exerciseResultClassCountsAsEfficacy(class string) bool {
	switch strings.TrimSpace(class) {
	case string(llm.ExerciseOutcomeTransportError), string(llm.ExerciseOutcomeProviderTimeout), string(llm.ExerciseOutcomeValidityAmbiguous):
		return false
	default:
		return true
	}
}

func legacyBlankZeroFailure(row db.ExerciseResultRow) bool {
	return strings.TrimSpace(row.ResultClass) == "" &&
		strings.TrimSpace(row.ErrorMsg) == "" &&
		strings.TrimSpace(row.Content) == "" &&
		!row.Passed &&
		row.Quality == 0
}
