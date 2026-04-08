package routes

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
)

// GetTurnContext returns context window analysis for a turn from context_snapshots.
func GetTurnContext(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "id")
		row := db.NewRouteQueries(store).GetTurnContext(r.Context(), turnID)
		var complexity, model string
		var budget int64
		var sysTokens, memTokens, histTokens, histDepth *int64
		err := row.Scan(&complexity, &budget, &sysTokens, &memTokens, &histTokens, &histDepth, &model)
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "turn context not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to query turn context")
			return
		}
		st := derefInt64(sysTokens)
		mt := derefInt64(memTokens)
		ht := derefInt64(histTokens)
		writeJSON(w, http.StatusOK, map[string]any{
			"system_tokens":        st,
			"system_prompt_tokens": st,
			"memory_tokens":        mt,
			"history_tokens":       ht,
			"total_tokens":         st + mt + ht,
			"max_tokens":           budget,
			"token_budget":         budget,
			"complexity_level":     complexity,
			"history_depth":        derefInt64(histDepth),
			"model":                model,
			"snapshot":             true,
		})
	}
}

// PutTurnFeedback updates an existing turn feedback row.
func PutTurnFeedback(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct {
			Grade   int    `json:"grade"`
			Comment string `json:"comment"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Grade < 1 || req.Grade > 5 {
			writeError(w, http.StatusBadRequest, "grade must be between 1 and 5")
			return
		}
		n, err := db.NewRouteQueries(store).UpdateTurnFeedback(r.Context(), id, req.Grade, req.Comment)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if n == 0 {
			writeError(w, http.StatusNotFound, "feedback not found for this turn")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "turn_id": id})
	}
}

// GetTurnTools returns tool calls for a turn from the tool_calls table.
func GetTurnTools(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "id")
		rows, err := db.NewRouteQueries(store).GetTurnToolsDetailed(r.Context(), turnID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query turn tools")
			return
		}
		defer func() { _ = rows.Close() }()

		calls := make([]map[string]any, 0)
		for rows.Next() {
			var id, toolName, input, status, createdAt string
			var output, skillName *string
			var durationMs *int64
			if err := rows.Scan(&id, &toolName, &input, &output, &status, &durationMs, &skillName, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read turn tool row")
				return
			}
			c := map[string]any{
				"id": id, "tool_name": toolName, "input": input,
				"status": status, "created_at": createdAt,
			}
			if output != nil {
				c["output"] = *output
			}
			if durationMs != nil {
				c["duration_ms"] = *durationMs
			}
			if skillName != nil {
				c["skill_name"] = *skillName
			}
			calls = append(calls, c)
		}
		writeJSON(w, http.StatusOK, map[string]any{"tool_calls": calls})
	}
}

// tipSeverity maps a tip type to its severity level.
func tipSeverity(tipType string) string {
	switch tipType {
	case "error":
		return "error"
	case "warning":
		return "warning"
	default:
		return "info"
	}
}

// GetTurnTips returns optimization tips for a turn based on inference data.
func GetTurnTips(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "id")
		tips := make([]map[string]string, 0)

		// Check if this turn used excessive tokens.
		row := db.NewRouteQueries(store).GetTurnTokens(r.Context(), turnID)
		var tokIn, tokOut int64
		var cost float64
		if row.Scan(&tokIn, &tokOut, &cost) == nil {
			if tokIn > 4000 {
				msg := "High input tokens — consider trimming context window"
				tips = append(tips, map[string]string{
					"type": "optimization", "message": msg,
					"severity": tipSeverity("optimization"), "suggestion": msg,
				})
			}
			if tokOut > 2000 {
				msg := "High output tokens — consider setting max_tokens"
				tips = append(tips, map[string]string{
					"type": "optimization", "message": msg,
					"severity": tipSeverity("optimization"), "suggestion": msg,
				})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"tips": tips})
	}
}

// GetTurnModelSelection returns model selection details for a turn.
func GetTurnModelSelection(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "id")
		row := db.NewRouteQueries(store).GetTurnModelSelection(r.Context(), turnID)
		var id, model, strategy, primary, createdAt string
		var override, complexity, candidatesJSON, attribution *string
		err := row.Scan(&id, &model, &strategy, &primary, &override, &complexity, &candidatesJSON, &attribution, &createdAt)
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "turn model selection not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to query turn model selection")
			return
		}
		result := map[string]any{
			"id": id, "selected_model": model, "strategy": strategy,
			"primary_model": primary, "created_at": createdAt,
		}
		if override != nil {
			result["override_model"] = *override
		}
		if complexity != nil {
			result["complexity"] = *complexity
		}
		if attribution != nil {
			result["attribution"] = *attribution
		}
		if candidatesJSON != nil {
			var candidates any
			if json.Unmarshal([]byte(*candidatesJSON), &candidates) == nil {
				result["candidates"] = candidates
			}
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// AnalyzeTurn returns turn analysis based on available data.
// When an LLM service is available, heuristic tips are sent to the LLM
// for a deeper remediation analysis matching the Rust behavior.
func AnalyzeTurn(store *db.Store, llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "id")
		ctx := r.Context()

		// Gather turn data.
		rq := db.NewRouteQueries(store)
		row := rq.GetTurnForAnalysis(ctx, turnID)
		var model string
		var tokIn, tokOut int64
		var cost float64
		if err := row.Scan(&model, &tokIn, &tokOut, &cost); err != nil {
			writeError(w, http.StatusNotFound, "turn not found")
			return
		}
		// Cached flag is optional (column may not exist in older schemas).
		var cached int
		_ = rq.TurnCachedFlag(ctx, turnID).Scan(&cached)

		// Count tool calls and failures.
		var toolCount, toolFails int64
		_ = rq.ToolCallCountsForTurn(ctx, turnID).Scan(&toolCount, &toolFails)

		// Load context snapshot if available.
		var tokenBudget, sysTok, memTok, histTok, histDepth int64
		var complexLevel string
		_ = rq.GetContextSnapshotForAnalysis(ctx, turnID).
			Scan(&tokenBudget, &sysTok, &memTok, &histTok, &histDepth, &complexLevel)

		// Build TurnData and run analyzer.
		td := &pipeline.TurnData{
			TurnID:             turnID,
			TokenBudget:        tokenBudget,
			SystemPromptTokens: sysTok,
			MemoryTokens:       memTok,
			HistoryTokens:      histTok,
			HistoryDepth:       histDepth,
			ComplexityLevel:    complexLevel,
			Model:              model,
			Cost:               cost,
			TokensIn:           tokIn,
			TokensOut:          tokOut,
			ToolCallCount:      toolCount,
			ToolFailureCount:   toolFails,
			Cached:             cached == 1,
		}

		analyzer := pipeline.NewContextAnalyzer()
		tips := analyzer.AnalyzeTurn(td)

		// Build LLM analysis prompt from heuristic tips.
		var analysisText string
		var analysisModel string
		var analysisTokIn, analysisTokOut int64
		var analysisCost float64

		prompt := pipeline.BuildTurnAnalysisPrompt(td, tips)
		if llmSvc != nil {
			resp, err := llmSvc.Complete(ctx, &llm.Request{
				Messages:  []llm.Message{{Role: "user", Content: prompt}},
				MaxTokens: 1200,
			})
			if err == nil {
				analysisText = resp.Content
				analysisModel = resp.Model
				analysisTokIn = int64(resp.Usage.InputTokens)
				analysisTokOut = int64(resp.Usage.OutputTokens)
				_ = analysisCost // cost tracked by LLM service internally
			} else {
				analysisText = pipeline.BuildHeuristicSummary(tips)
			}
		} else {
			analysisText = pipeline.BuildHeuristicSummary(tips)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"turn_id":        turnID,
			"status":         "complete",
			"heuristic_tips": tips,
			"analysis":       analysisText,
			"analysis_model": analysisModel,
			"tokens_in":      analysisTokIn,
			"tokens_out":     analysisTokOut,
			"cost":           analysisCost,
		})
	}
}
