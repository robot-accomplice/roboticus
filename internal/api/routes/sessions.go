package routes

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
)

// ListSessions returns all sessions.
func ListSessions(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, agent_id, scope_key, status, nickname, created_at, updated_at
			 FROM sessions ORDER BY created_at DESC LIMIT 100`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer func() { _ = rows.Close() }()

		var sessions []map[string]any
		for rows.Next() {
			var id, agentID, scopeKey, status, createdAt, updatedAt string
			var nickname *string
			if err := rows.Scan(&id, &agentID, &scopeKey, &status, &nickname, &createdAt, &updatedAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read session row")
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
		// Append session ID to scope to make it unique per session.
		// The partial unique index on (agent_id, scope_key) WHERE status='active'
		// prevents duplicate active sessions with the same scope.
		scopeKey := req.Scope + ":" + id
		_, err := store.ExecContext(r.Context(),
			`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, ?, ?)`,
			id, req.AgentID, scopeKey,
		)
		if err != nil {
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
		row := store.QueryRowContext(r.Context(),
			`SELECT id, agent_id, scope_key, status, nickname, created_at, updated_at FROM sessions WHERE id = ?`, id)

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
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, role, content, created_at FROM session_messages
			 WHERE session_id = ? ORDER BY created_at ASC LIMIT 200`, sessionID)
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
func PostMessage(p pipeline.Runner) http.HandlerFunc {
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

		input := pipeline.Input{
			Content:   req.Content,
			SessionID: sessionID,
			AgentID:   req.AgentID,
			AgentName: "Roboticus",
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
		res, err := store.ExecContext(r.Context(),
			`UPDATE sessions SET status = 'archived' WHERE id = ?`, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			writeError(w, http.StatusNotFound, "session not found")
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
		rows, err := store.QueryContext(ctx,
			`SELECT s.id, (
				SELECT content FROM session_messages
				WHERE session_id = s.id AND role = 'user'
				ORDER BY created_at ASC LIMIT 1
			) AS first_msg
			FROM sessions s
			WHERE s.nickname IS NULL OR s.nickname = ''`)
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
			_, err := store.ExecContext(ctx,
				`UPDATE sessions SET nickname = ? WHERE id = ?`, nick, id)
			if err == nil {
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
		row := store.QueryRowContext(ctx,
			`SELECT created_at FROM sessions WHERE id = ?`, id)
		if err := row.Scan(&createdAt); err != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}

		// Load all turns for this session.
		turnRows, err := store.QueryContext(ctx,
			`SELECT t.id, COALESCE(t.model, ''), COALESCE(t.tokens_in, 0), COALESCE(t.tokens_out, 0),
			        COALESCE(t.cost, 0), COALESCE(t.cached, 0)
			 FROM turns t WHERE t.session_id = ? ORDER BY t.created_at`, id)
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
				TurnID:   turnID,
				Model:    model,
				TokensIn: tokIn,
				TokensOut: tokOut,
				Cost:     cost,
				Cached:   cached == 1,
			}

			// Load context snapshot for this turn.
			snapRow := store.QueryRowContext(ctx,
				`SELECT COALESCE(max_tokens, 0), COALESCE(system_tokens, 0), COALESCE(memory_tokens, 0),
				        COALESCE(history_tokens, 0), COALESCE(history_depth, 0)
				 FROM context_snapshots WHERE turn_id = ?`, turnID)
			_ = snapRow.Scan(&td.TokenBudget, &td.SystemPromptTokens, &td.MemoryTokens, &td.HistoryTokens, &td.HistoryDepth)

			// Count tool calls.
			_ = store.QueryRowContext(ctx,
				`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status != 'success' THEN 1 ELSE 0 END), 0)
				 FROM tool_calls WHERE turn_id = ?`, turnID).
				Scan(&td.ToolCallCount, &td.ToolFailureCount)

			turns = append(turns, td)
		}

		// Load feedback grades.
		gradeRows, err := store.QueryContext(ctx,
			`SELECT turn_id, COALESCE(grade, 0) FROM turn_feedback WHERE session_id = ?`, id)
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
