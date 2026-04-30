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

// ListSessions returns all sessions, optionally filtered to only those with flight records.
func ListSessions(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rq := db.NewRouteQueries(store)
		var rows *sql.Rows
		var err error
		if r.URL.Query().Get("has_flight_records") == "true" {
			rows, err = rq.ListSessionsWithFlightRecords(r.Context(), 100)
		} else {
			rows, err = rq.ListSessions(r.Context(), 100)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer func() { _ = rows.Close() }()

		var sessions []map[string]any
		for rows.Next() {
			var id, agentID, scopeKey, status, createdAt, updatedAt string
			var nickname *string
			var turnCount, messageCount, traceCount, snapshotCount, totalTokens int64
			var totalCost float64
			var lastActivityAt string
			if err := rows.Scan(
				&id,
				&agentID,
				&scopeKey,
				&status,
				&nickname,
				&createdAt,
				&updatedAt,
				&turnCount,
				&messageCount,
				&traceCount,
				&snapshotCount,
				&totalTokens,
				&totalCost,
				&lastActivityAt,
			); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read session row")
				return
			}
			s := map[string]any{
				"id":               id,
				"agent_id":         agentID,
				"scope":            scopeKey,
				"status":           status,
				"created_at":       createdAt,
				"updated_at":       updatedAt,
				"turn_count":       turnCount,
				"message_count":    messageCount,
				"trace_count":      traceCount,
				"snapshot_count":   snapshotCount,
				"total_tokens":     totalTokens,
				"total_cost":       totalCost,
				"last_activity_at": lastActivityAt,
			}
			if nickname != nil {
				s["nickname"] = *nickname
			}
			sessions = append(sessions, s)
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	}
}

// CreateSession creates a new session.
// The scope_key is made unique per session to avoid UNIQUE constraint violations
// on the (agent_id, scope_key) partial index for active sessions.
func CreateSession(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			AgentID string `json:"agent_id"`
			Scope   string `json:"scope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.AgentID == "" {
			req.AgentID = "default"
		}
		if req.Scope == "" {
			req.Scope = "api"
		}

		id := db.NewID()
		scopeKey := req.Scope + ":" + id
		repo := db.NewSessionRepository(store)
		if err := repo.CreateSession(r.Context(), id, req.AgentID, scopeKey); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{
			"id":       id,
			"agent_id": req.AgentID,
			"scope":    req.Scope,
		})
	}
}

// GetSession returns a single session.
func GetSession(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		row := db.NewRouteQueries(store).GetSession(r.Context(), id)

		var agentID, scopeKey, status, createdAt, updatedAt string
		var nickname *string
		if err := row.Scan(&id, &agentID, &scopeKey, &status, &nickname, &createdAt, &updatedAt); err != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}

		s := map[string]any{
			"id":         id,
			"agent_id":   agentID,
			"scope":      scopeKey,
			"status":     status,
			"created_at": createdAt,
			"updated_at": updatedAt,
		}
		if nickname != nil {
			s["nickname"] = *nickname
		}
		writeJSON(w, http.StatusOK, s)
	}
}

// ListMessages returns messages for a session.
func ListMessages(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		rows, err := db.NewRouteQueries(store).SessionMessages(r.Context(), sessionID, 200)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer func() { _ = rows.Close() }()

		var messages []map[string]string
		for rows.Next() {
			var id, role, content, createdAt string
			if err := rows.Scan(&id, &role, &content, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read session message row")
				return
			}
			messages = append(messages, map[string]string{
				"id":         id,
				"role":       role,
				"content":    content,
				"created_at": createdAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
	}
}

// PostMessage sends a message to a session via the pipeline.
func PostMessage(p pipeline.Runner, agentName ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "id")
		var req struct {
			Content string `json:"content"`
			AgentID string `json:"agent_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Content == "" {
			writeError(w, http.StatusBadRequest, "content is required")
			return
		}
		if req.AgentID == "" {
			req.AgentID = "default"
		}

		name := "Roboticus"
		if len(agentName) > 0 && agentName[0] != "" {
			name = agentName[0]
		}
		input := pipeline.Input{
			Content:   req.Content,
			SessionID: sessionID,
			AgentID:   req.AgentID,
			AgentName: name,
			Platform:  "api",
		}

		outcome, err := pipeline.RunPipeline(r.Context(), p, pipeline.PresetAPI(), input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, outcome)
	}
}

// ArchiveSession sets a session's status to "archived".
func ArchiveSession(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		repo := db.NewSessionRepository(store)
		if err := repo.ArchiveSession(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "archived", "id": id})
	}
}

// BackfillNicknames generates nicknames for sessions that lack them.
// It uses the first user message (truncated to 50 chars) as the nickname.
func BackfillNicknames(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		rows, err := db.NewRouteQueries(store).SessionsWithoutNicknames(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer func() { _ = rows.Close() }()

		var updated int
		for rows.Next() {
			var id string
			var firstMsg *string
			if err := rows.Scan(&id, &firstMsg); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read nickname backfill row")
				return
			}
			nick := "Untitled"
			if firstMsg != nil && *firstMsg != "" {
				nick = *firstMsg
				if len(nick) > 50 {
					nick = nick[:50]
				}
			}
			repo := db.NewSessionRepository(store)
			if err := repo.SetNickname(ctx, id, nick); err == nil {
				updated++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated": updated})
	}
}

// AnalyzeSession returns basic analytics for a session.
func AnalyzeSession(store *db.Store, llmSvc *llm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		ctx := r.Context()

		// Verify session exists.
		var createdAt string
		row := db.NewRouteQueries(store).SessionExists(ctx, id)
		if err := row.Scan(&createdAt); err != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}

		// Load all turns for this session.
		turnRows, err := db.NewRouteQueries(store).ListTurnsForAnalysis(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query turns")
			return
		}
		defer func() { _ = turnRows.Close() }()

		var turns []pipeline.TurnData
		for turnRows.Next() {
			var turnID, model string
			var tokIn, tokOut int64
			var cost float64
			var cached int
			if err := turnRows.Scan(&turnID, &model, &tokIn, &tokOut, &cost, &cached); err != nil {
				continue
			}

			td := pipeline.TurnData{
				TurnID:    turnID,
				Model:     model,
				TokensIn:  tokIn,
				TokensOut: tokOut,
				Cost:      cost,
				Cached:    cached == 1,
			}

			// Load context snapshot for this turn.
			snapRow := db.NewRouteQueries(store).ContextSnapshotForTurn(ctx, turnID)
			_ = snapRow.Scan(&td.TokenBudget, &td.SystemPromptTokens, &td.MemoryTokens, &td.HistoryTokens, &td.HistoryDepth)

			// Count tool calls.
			_ = db.NewRouteQueries(store).ToolCallCountsForTurn(ctx, turnID).
				Scan(&td.ToolCallCount, &td.ToolFailureCount)

			turns = append(turns, td)
		}

		// Load feedback grades.
		gradeRows, err := db.NewRouteQueries(store).SessionFeedbackGrades(ctx, id)
		var grades []pipeline.SessionGrade
		if err == nil {
			defer func() { _ = gradeRows.Close() }()
			for gradeRows.Next() {
				var turnID string
				var grade int
				if gradeRows.Scan(&turnID, &grade) == nil {
					grades = append(grades, pipeline.SessionGrade{TurnID: turnID, Grade: grade})
				}
			}
		}

		// Run session analysis.
		sd := &pipeline.SessionData{
			SessionID: id,
			Turns:     turns,
			Grades:    grades,
		}
		analyzer := pipeline.NewContextAnalyzer()
		tips := analyzer.AnalyzeSession(sd)

		// Also run per-turn tips for aggregation.
		for i := range turns {
			turnTips := analyzer.AnalyzeTurn(&turns[i])
			tips = append(tips, turnTips...)
		}

		// Deduplicate by rule name, keeping highest severity.
		seen := make(map[string]pipeline.Tip)
		severityOrder := map[string]int{"critical": 3, "warning": 2, "info": 1}
		for _, tip := range tips {
			if existing, ok := seen[tip.RuleName]; !ok || severityOrder[tip.Severity] > severityOrder[existing.Severity] {
				seen[tip.RuleName] = tip
			}
		}
		var dedupedTips []pipeline.Tip
		for _, tip := range seen {
			dedupedTips = append(dedupedTips, tip)
		}

		var critCount, warnCount int
		for _, tip := range dedupedTips {
			switch tip.Severity {
			case "critical":
				critCount++
			case "warning":
				warnCount++
			}
		}

		// Build LLM analysis prompt from heuristic tips.
		var analysisText string
		var analysisModel string
		var analysisTokIn, analysisTokOut int64
		var analysisCost float64

		prompt := pipeline.BuildSessionAnalysisPrompt(id, turns, dedupedTips, grades)
		if llmSvc != nil {
			resp, err := llmSvc.Complete(ctx, &llm.Request{
				Messages:  []llm.Message{{Role: "user", Content: prompt}},
				MaxTokens: 1800,
			})
			if err == nil {
				analysisText = resp.Content
				analysisModel = resp.Model
				analysisTokIn = int64(resp.Usage.InputTokens)
				analysisTokOut = int64(resp.Usage.OutputTokens)
				_ = analysisCost // cost tracked by LLM service internally
			} else {
				analysisText = pipeline.BuildHeuristicSummary(dedupedTips)
			}
		} else {
			analysisText = pipeline.BuildHeuristicSummary(dedupedTips)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"session_id":         id,
			"status":             "complete",
			"heuristic_insights": dedupedTips,
			"analysis":           analysisText,
			"analysis_model":     analysisModel,
			"tokens_in":          analysisTokIn,
			"tokens_out":         analysisTokOut,
			"cost":               analysisCost,
		})
	}
}
