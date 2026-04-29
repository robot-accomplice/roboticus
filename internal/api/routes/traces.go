package routes

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/db"
	"roboticus/internal/pipeline"
)

// ListTraces returns recent pipeline traces.
func ListTraces(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 50)
		rows, err := db.NewRouteQueries(store).ListTracesSimple(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query traces")
			return
		}
		defer func() { _ = rows.Close() }()

		traces := make([]map[string]any, 0)
		for rows.Next() {
			var id, turnID, channel, stagesJSON, createdAt string
			var totalMs int64
			if err := rows.Scan(&id, &turnID, &channel, &totalMs, &stagesJSON, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read trace row")
				return
			}
			traces = append(traces, map[string]any{
				"id": id, "turn_id": turnID, "channel": channel,
				"total_ms": totalMs, "created_at": createdAt,
				"health": pipelineHealthFromStages(stagesJSON),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"route_family": "traces",
			"artifact":     "trace_summary_list",
			"fidelity":     "summary",
			"traces":       traces,
		})
	}
}

// SearchTraces searches traces by optional tool, guard, duration, and timestamp filters.
func SearchTraces(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseIntParam(r, "limit", 50)
		if limit > 200 {
			limit = 200
		}
		toolName := strings.TrimSpace(r.URL.Query().Get("tool_name"))
		guardName := strings.TrimSpace(r.URL.Query().Get("guard_name"))
		minDuration := parseIntParam(r, "min_duration_ms", 0)
		since := strings.TrimSpace(r.URL.Query().Get("since"))

		query := `SELECT turn_id, session_id, channel, total_ms, created_at, stages_json, COALESCE(react_trace_json, '')
			FROM pipeline_traces WHERE 1=1`
		args := make([]any, 0, 5)
		if toolName != "" {
			query += ` AND EXISTS (
				SELECT 1 FROM tool_calls tc
				WHERE tc.turn_id = pipeline_traces.turn_id
				  AND tc.tool_name = ?
			)`
			args = append(args, toolName)
		}
		if minDuration > 0 {
			query += ` AND total_ms >= ?`
			args = append(args, minDuration)
		}
		if since != "" {
			query += ` AND created_at >= ?`
			args = append(args, since)
		}
		query += ` ORDER BY created_at DESC`
		if guardName == "" {
			query += ` LIMIT ?`
			args = append(args, limit)
		}

		rows, err := db.NewRouteQueries(store).Query(r.Context(), query, args...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query traces")
			return
		}
		defer func() { _ = rows.Close() }()

		results := make([]map[string]any, 0)
		for rows.Next() {
			var turnID, sessionID, channel, createdAt, stagesJSON, reactTraceJSON string
			var totalMs int64
			if err := rows.Scan(&turnID, &sessionID, &channel, &totalMs, &createdAt, &stagesJSON, &reactTraceJSON); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read trace search row")
				return
			}
			if guardName != "" && !traceContainsGuard(stagesJSON, reactTraceJSON, guardName) {
				continue
			}
			results = append(results, map[string]any{
				"turn_id":     turnID,
				"session_id":  sessionID,
				"channel":     channel,
				"total_ms":    totalMs,
				"created_at":  createdAt,
				"stages_json": stagesJSON,
			})
			if len(results) >= limit {
				break
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"route_family": "traces",
			"artifact":     "trace_search_results",
			"fidelity":     "summary_search",
			"results":      results,
			"count":        len(results),
		})
	}
}

func traceContainsGuard(stagesJSON, reactTraceJSON, guardName string) bool {
	needle := strings.ToLower(strings.TrimSpace(guardName))
	if needle == "" {
		return true
	}
	return jsonBlobContainsNeedle(stagesJSON, needle) || jsonBlobContainsNeedle(reactTraceJSON, needle)
}

func jsonBlobContainsNeedle(blob, needle string) bool {
	if strings.TrimSpace(blob) == "" {
		return false
	}
	var parsed any
	if err := json.Unmarshal([]byte(blob), &parsed); err != nil {
		return strings.Contains(strings.ToLower(blob), needle)
	}
	return jsonValueContainsNeedle(parsed, needle)
}

func jsonValueContainsNeedle(v any, needle string) bool {
	switch x := v.(type) {
	case string:
		return strings.Contains(strings.ToLower(x), needle)
	case []any:
		for _, item := range x {
			if jsonValueContainsNeedle(item, needle) {
				return true
			}
		}
	case map[string]any:
		for key, item := range x {
			if strings.Contains(strings.ToLower(key), needle) || jsonValueContainsNeedle(item, needle) {
				return true
			}
		}
	}
	return false
}

// GetTrace returns a pipeline trace by turn ID with parsed stages.
func GetTrace(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		row := db.NewRouteQueries(store).GetTraceByTurnID(r.Context(), turnID)
		var id, tid, channel, stagesJSON, createdAt string
		var totalMs int64
		err := row.Scan(&id, &tid, &channel, &totalMs, &stagesJSON, &createdAt)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "trace not found"})
			return
		}

		var stages any
		if json.Unmarshal([]byte(stagesJSON), &stages) != nil {
			stages = []any{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"route_family": "traces",
			"artifact":     "trace_detail",
			"fidelity":     "detail",
			"id":           id,
			"turn_id":      tid,
			"channel":      channel,
			"total_ms":     totalMs,
			"health":       pipelineHealthFromStages(stagesJSON),
			"stages":       stages,
			"created_at":   createdAt,
		})
	}
}

// GetReactTrace returns the ReAct trace for a given turn.
func GetReactTrace(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		// First find the pipeline trace for this turn, then get the react trace.
		row := db.NewRouteQueries(store).GetReactTraceByTurnID(r.Context(), turnID)
		var id, pipelineTraceID, reactJSON, createdAt string
		err := row.Scan(&id, &pipelineTraceID, &reactJSON, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "react trace not found")
			return
		}

		var parsed any
		if json.Unmarshal([]byte(reactJSON), &parsed) != nil {
			parsed = map[string]any{}
		}

		// Ensure steps is an array for JS (data.steps || []).
		steps := parsed
		if _, ok := parsed.([]any); !ok {
			steps = []any{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"route_family":      "traces",
			"artifact":          "react_trace_detail",
			"fidelity":          "detail",
			"id":                id,
			"pipeline_trace_id": pipelineTraceID,
			"react_trace":       parsed,
			"steps":             steps,
			"created_at":        createdAt,
		})
	}
}

// ExportTrace returns the full trace as downloadable JSON.
func ExportTrace(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		row := db.NewRouteQueries(store).GetTraceByTurnID(r.Context(), turnID)
		var id, tid, channel, stagesJSON, createdAt string
		var totalMs int64
		err := row.Scan(&id, &tid, &channel, &totalMs, &stagesJSON, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "trace not found")
			return
		}

		var stages any
		if json.Unmarshal([]byte(stagesJSON), &stages) != nil {
			stages = []any{}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=trace-"+turnID+".json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": id, "turn_id": tid, "channel": channel,
			"total_ms": totalMs, "stages": stages, "created_at": createdAt,
		})
	}
}

// ReplayTrace returns a replay preview for a given trace turn.
func ReplayTrace(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		row := db.NewRouteQueries(store).GetTraceByTurnID(r.Context(), turnID)
		var id, tid, channel, stagesJSON, createdAt string
		var totalMs int64
		err := row.Scan(&id, &tid, &channel, &totalMs, &stagesJSON, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "trace not found")
			return
		}

		var stages any
		if json.Unmarshal([]byte(stagesJSON), &stages) != nil {
			stages = []any{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"turn_id":  tid,
			"replayed": true,
			"trace": map[string]any{
				"id": id, "turn_id": tid, "channel": channel,
				"total_ms": totalMs, "health": pipelineHealthFromStages(stagesJSON),
				"stages": stages, "created_at": createdAt,
			},
		})
	}
}

// GetTraceFlow returns trace stages with timing diagram data.
func GetTraceFlow(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		row := db.NewRouteQueries(store).GetTraceByTurnID(r.Context(), turnID)
		var id, tid, channel, stagesJSON, createdAt string
		var totalMs int64
		err := row.Scan(&id, &tid, &channel, &totalMs, &stagesJSON, &createdAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "trace not found")
			return
		}

		var stages any
		if json.Unmarshal([]byte(stagesJSON), &stages) != nil {
			stages = []any{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "turn_id": tid, "channel": channel,
			"total_ms": totalMs, "health": pipelineHealthFromStages(stagesJSON),
			"stages": stages, "created_at": createdAt,
			"format": "flow",
		})
	}
}

// GetTurnDiagnostics returns the canonical RCA artifact for a turn.
func GetTurnDiagnostics(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		turnID := chi.URLParam(r, "turn_id")
		row := db.NewRouteQueries(store).GetTurnDiagnostics(r.Context(), turnID)
		var (
			id, tid, sessionID, channel, status, finalModel, finalProvider            string
			totalMs, inferenceAttempts, fallbackCount, toolCallCount                  int64
			guardRetryCount, verifierRetryCount, replaySuppressionCount               int64
			requestMessages, requestTools, requestApproxTokens                        int64
			contextPressure, resourcePressure, resourceSnapshotJSON, primaryDiagnosis string
			userNarrative, operatorNarrative, recommendationsJSON                     string
			createdAt                                                                 string
			diagnosisConfidence                                                       float64
		)
		if err := row.Scan(
			&id, &tid, &sessionID, &channel, &status, &finalModel, &finalProvider,
			&totalMs, &inferenceAttempts, &fallbackCount, &toolCallCount,
			&guardRetryCount, &verifierRetryCount, &replaySuppressionCount, &requestMessages, &requestTools,
			&requestApproxTokens, &contextPressure, &resourcePressure, &resourceSnapshotJSON, &primaryDiagnosis,
			&diagnosisConfidence, &userNarrative, &operatorNarrative, &recommendationsJSON, &createdAt,
		); err != nil {
			writeError(w, http.StatusNotFound, "turn diagnostics not found")
			return
		}

		eventsRows, err := db.NewRouteQueries(store).ListTurnDiagnosticEvents(r.Context(), turnID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query turn diagnostic events")
			return
		}
		defer func() { _ = eventsRows.Close() }()

		events := make([]map[string]any, 0)
		typedEvents := make([]pipeline.TurnDiagnosticEvent, 0)
		for eventsRows.Next() {
			var (
				eventID, eventTurnID, eventType, parentEventID, eventStatus string
				operatorSummary, userSummary, detailsJSON, eventCreatedAt   string
				seq, atMs, durationMs                                       int64
			)
			if err := eventsRows.Scan(
				&eventID, &eventTurnID, &seq, &eventType, &atMs, &durationMs,
				&parentEventID, &eventStatus, &operatorSummary, &userSummary,
				&detailsJSON, &eventCreatedAt,
			); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read turn diagnostic event row")
				return
			}
			var detailsMap map[string]any
			if strings.TrimSpace(detailsJSON) != "" {
				if json.Unmarshal([]byte(detailsJSON), &detailsMap) != nil {
					detailsMap = nil
				}
			}
			ev := map[string]any{
				"id":               eventID,
				"turn_id":          eventTurnID,
				"seq":              seq,
				"event_type":       eventType,
				"at_ms":            atMs,
				"duration_ms":      durationMs,
				"status":           eventStatus,
				"operator_summary": operatorSummary,
				"user_summary":     userSummary,
				"created_at":       eventCreatedAt,
			}
			if parentEventID != "" {
				ev["parent_event_id"] = parentEventID
			}
			if detailsMap != nil {
				ev["details"] = detailsMap
			}
			events = append(events, ev)
			typedEvents = append(typedEvents, pipeline.TurnDiagnosticEvent{
				EventID:         eventID,
				TurnID:          eventTurnID,
				Seq:             int(seq),
				Type:            eventType,
				AtMs:            atMs,
				DurationMs:      durationMs,
				ParentEventID:   parentEventID,
				Status:          eventStatus,
				OperatorSummary: operatorSummary,
				UserSummary:     userSummary,
				Details:         detailsMap,
			})
		}

		typedSummary := pipeline.TurnDiagnosticSummary{
			TurnID:                 tid,
			SessionID:              sessionID,
			Channel:                channel,
			Status:                 status,
			FinalModel:             finalModel,
			FinalProvider:          finalProvider,
			TotalMs:                totalMs,
			InferenceAttempts:      int(inferenceAttempts),
			FallbackCount:          int(fallbackCount),
			ToolCallCount:          int(toolCallCount),
			GuardRetryCount:        int(guardRetryCount),
			VerifierRetryCount:     int(verifierRetryCount),
			ReplaySuppressionCount: int(replaySuppressionCount),
			RequestMessages:        int(requestMessages),
			RequestTools:           int(requestTools),
			RequestApproxTokens:    int(requestApproxTokens),
			ContextPressure:        contextPressure,
			ResourcePressure:       resourcePressure,
			ResourceSnapshotJSON:   resourceSnapshotJSON,
			PrimaryDiagnosis:       primaryDiagnosis,
			DiagnosisConfidence:    diagnosisConfidence,
			UserNarrative:          userNarrative,
			OperatorNarrative:      operatorNarrative,
			RecommendationsJSON:    recommendationsJSON,
		}
		typedSummary = pipeline.DeriveInterpretiveDiagnosticsSummary(typedSummary, typedEvents)

		summary := map[string]any{
			"id":                       id,
			"turn_id":                  tid,
			"session_id":               sessionID,
			"channel":                  channel,
			"status":                   status,
			"final_model":              finalModel,
			"final_provider":           finalProvider,
			"total_ms":                 totalMs,
			"inference_attempts":       inferenceAttempts,
			"fallback_count":           fallbackCount,
			"tool_call_count":          toolCallCount,
			"guard_retry_count":        guardRetryCount,
			"verifier_retry_count":     verifierRetryCount,
			"replay_suppression_count": replaySuppressionCount,
			"request_messages":         requestMessages,
			"request_tools":            requestTools,
			"request_approx_tokens":    requestApproxTokens,
			"context_pressure":         contextPressure,
			"resource_pressure":        resourcePressure,
			"primary_diagnosis":        primaryDiagnosis,
			"diagnosis_confidence":     diagnosisConfidence,
			"user_narrative":           typedSummary.UserNarrative,
			"operator_narrative":       typedSummary.OperatorNarrative,
			"created_at":               createdAt,
		}
		if strings.TrimSpace(recommendationsJSON) != "" {
			var recommendations any
			if json.Unmarshal([]byte(recommendationsJSON), &recommendations) == nil {
				summary["recommendations"] = recommendations
			}
		}
		if strings.TrimSpace(resourceSnapshotJSON) != "" {
			var resourceSnapshot any
			if json.Unmarshal([]byte(resourceSnapshotJSON), &resourceSnapshot) == nil {
				summary["resource_snapshot"] = resourceSnapshot
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"route_family": "traces",
			"artifact":     "turn_diagnostics",
			"fidelity":     "macro_detail",
			"turn_id":      tid,
			"summary":      summary,
			"events":       events,
		})
	}
}
