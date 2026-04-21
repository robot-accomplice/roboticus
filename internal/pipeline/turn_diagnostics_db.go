package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

func (p *Pipeline) appendTurnDiagnosticEvent(ctx context.Context, turnID, eventType, status, operatorSummary, userSummary string, details map[string]any) {
	if p.store == nil || strings.TrimSpace(turnID) == "" {
		return
	}

	var atMs int64
	err := p.store.QueryRowContext(ctx,
		`SELECT COALESCE(total_ms, 0)
		   FROM turn_diagnostics
		  WHERE turn_id = ?
		  LIMIT 1`,
		turnID,
	).Scan(&atMs)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Debug().Err(err).Str("turn", turnID).Str("event_type", eventType).Msg("failed to load turn diagnostics summary for append")
		}
		return
	}

	var nextSeq int
	if err := p.store.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1
		   FROM turn_diagnostic_events
		  WHERE turn_id = ?`,
		turnID,
	).Scan(&nextSeq); err != nil {
		log.Debug().Err(err).Str("turn", turnID).Str("event_type", eventType).Msg("failed to compute next diagnostic sequence")
		return
	}

	var lastAtMs sql.NullInt64
	if err := p.store.QueryRowContext(ctx,
		`SELECT MAX(at_ms)
		   FROM turn_diagnostic_events
		  WHERE turn_id = ?`,
		turnID,
	).Scan(&lastAtMs); err == nil && lastAtMs.Valid && lastAtMs.Int64 >= atMs {
		atMs = lastAtMs.Int64 + 1
	}

	detailsJSON := ""
	if len(details) > 0 {
		if buf, err := json.Marshal(details); err == nil {
			detailsJSON = string(buf)
		}
	}

	if _, err := p.store.ExecContext(ctx,
		`INSERT INTO turn_diagnostic_events (
			id, turn_id, seq, event_type, at_ms, duration_ms, parent_event_id, status,
			operator_summary, user_summary, details_json
		) VALUES (?, ?, ?, ?, ?, 0, '', ?, ?, ?, ?)`,
		db.NewID(), turnID, nextSeq, eventType, atMs, status, operatorSummary, userSummary, detailsJSON,
	); err != nil {
		log.Debug().Err(err).Str("turn", turnID).Str("event_type", eventType).Msg("failed to append turn diagnostic event")
	}
}

func (p *Pipeline) storeTurnDiagnostics(ctx context.Context, dr *TurnDiagnosticsRecorder) {
	if p.store == nil || dr == nil {
		return
	}

	summary, events, ok := dr.SnapshotForFlush("")
	if !ok {
		return
	}
	if summary.TurnID == "" {
		return
	}
	if summary.Status == "" {
		summary.Status = diagnosticsStatusFromEvents(events)
	}
	summary = DeriveInterpretiveDiagnosticsSummary(summary, events)

	var existingID string
	err := p.store.QueryRowContext(ctx,
		`SELECT id FROM turn_diagnostics WHERE turn_id = ? LIMIT 1`,
		summary.TurnID,
	).Scan(&existingID)
	switch {
	case err == nil:
		_, err = p.store.ExecContext(ctx,
			`UPDATE turn_diagnostics SET
				session_id = ?, channel = ?, status = ?, final_model = ?, final_provider = ?, total_ms = ?,
				inference_attempts = ?, fallback_count = ?, tool_call_count = ?, guard_retry_count = ?, verifier_retry_count = ?, replay_suppression_count = ?,
				request_messages = ?, request_tools = ?, request_approx_tokens = ?, context_pressure = ?, resource_pressure = ?,
				resource_snapshot_json = ?, primary_diagnosis = ?, diagnosis_confidence = ?, user_narrative = ?, operator_narrative = ?, recommendations_json = ?
			WHERE turn_id = ?`,
			summary.SessionID, summary.Channel, summary.Status, summary.FinalModel, summary.FinalProvider, summary.TotalMs,
			summary.InferenceAttempts, summary.FallbackCount, summary.ToolCallCount, summary.GuardRetryCount, summary.VerifierRetryCount, summary.ReplaySuppressionCount,
			summary.RequestMessages, summary.RequestTools, summary.RequestApproxTokens, summary.ContextPressure, summary.ResourcePressure,
			summary.ResourceSnapshotJSON, summary.PrimaryDiagnosis, summary.DiagnosisConfidence, summary.UserNarrative, summary.OperatorNarrative, summary.RecommendationsJSON,
			summary.TurnID,
		)
	case err == sql.ErrNoRows:
		_, err = p.store.ExecContext(ctx,
			`INSERT INTO turn_diagnostics (
				id, turn_id, session_id, channel, status, final_model, final_provider, total_ms,
				inference_attempts, fallback_count, tool_call_count, guard_retry_count, verifier_retry_count, replay_suppression_count,
				request_messages, request_tools, request_approx_tokens, context_pressure, resource_pressure,
				resource_snapshot_json, primary_diagnosis, diagnosis_confidence, user_narrative, operator_narrative, recommendations_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			db.NewID(), summary.TurnID, summary.SessionID, summary.Channel, summary.Status,
			summary.FinalModel, summary.FinalProvider, summary.TotalMs, summary.InferenceAttempts,
			summary.FallbackCount, summary.ToolCallCount, summary.GuardRetryCount, summary.VerifierRetryCount, summary.ReplaySuppressionCount,
			summary.RequestMessages, summary.RequestTools, summary.RequestApproxTokens, summary.ContextPressure,
			summary.ResourcePressure, summary.ResourceSnapshotJSON, summary.PrimaryDiagnosis, summary.DiagnosisConfidence,
			summary.UserNarrative, summary.OperatorNarrative, summary.RecommendationsJSON,
		)
	default:
		// leave err as-is
	}
	if err != nil {
		log.Warn().Err(err).Str("turn", summary.TurnID).Msg("failed to store turn diagnostics summary")
		return
	}

	for _, ev := range events {
		detailsJSON := ""
		if len(ev.Details) > 0 {
			if buf, err := json.Marshal(ev.Details); err == nil {
				detailsJSON = string(buf)
			}
		}
		_, err := p.store.ExecContext(ctx,
			`INSERT INTO turn_diagnostic_events (
				id, turn_id, seq, event_type, at_ms, duration_ms, parent_event_id, status,
				operator_summary, user_summary, details_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ev.EventID, ev.TurnID, ev.Seq, ev.Type, ev.AtMs, ev.DurationMs, ev.ParentEventID,
			ev.Status, ev.OperatorSummary, ev.UserSummary, detailsJSON,
		)
		if err != nil {
			log.Warn().Err(err).Str("turn", summary.TurnID).Str("event_type", ev.Type).Msg("failed to store turn diagnostics event")
			return
		}
	}
}

func DeriveInterpretiveDiagnosticsSummary(summary TurnDiagnosticSummary, events []TurnDiagnosticEvent) TurnDiagnosticSummary {
	if !needsDerivedNarrative(summary.UserNarrative) && !needsDerivedNarrative(summary.OperatorNarrative) {
		return summary
	}
	attempts := diagnosticAttemptsFromEvents(events)
	firstAttemptSucceeded := len(attempts) > 0 && attempts[0].Status == "ok"
	distinctRoutes := diagnosticDistinctRoutes(attempts)
	guardRetry := latestDiagnosticEvent(events, "guard_retry_scheduled")
	verifierRetry := latestDiagnosticEvent(events, "verifier_retry_scheduled")
	loopTermination := latestDiagnosticEvent(events, "loop_terminated")
	replaySuppressed := latestDiagnosticEvent(events, "tool_call_replay_suppressed")
	guardName := firstViolationName(guardRetry, latestDiagnosticEvent(events, "response_finalized"))
	retryReason := diagnosticDetailString(guardRetry, "retry_reason")
	if retryReason == "" {
		retryReason = diagnosticDetailString(verifierRetry, "retry_reason")
	}
	finalRoute := diagnosticFinalRoute(summary, attempts)
	hostClause := ""
	if pressure := strings.TrimSpace(summary.ResourcePressure); pressure != "" && pressure != "unknown" {
		hostClause = fmt.Sprintf(" Host pressure: %s.", pressure)
	}

	userNarrative := summary.UserNarrative
	if needsDerivedNarrative(userNarrative) {
		switch {
		case replaySuppressed != nil || summary.ReplaySuppressionCount > 0:
			toolName := diagnosticDetailString(replaySuppressed, "tool_name")
			if toolName == "" {
				toolName = "the side-effecting tool"
			}
			replayReason := diagnosticDetailString(replaySuppressed, "reason")
			if replayReason == "" {
				replayReason = "a prior successful execution made the duplicate call replay-risky"
			}
			userNarrative = fmt.Sprintf("The turn completed after the framework suppressed %d duplicate replay-risky tool call(s). %s was not executed again because %s.%s",
				maxDiagnosticInt(summary.ReplaySuppressionCount, boolToInt(replaySuppressed != nil)),
				toolName,
				replayReason,
				hostClause)
		case loopTermination != nil && diagnosticDetailString(loopTermination, "reason_code") == "same_route_no_progress":
			userNarrative = fmt.Sprintf("The framework stopped repeated same-route attempts on %s because they were not making progress.%s", finalRoute, hostClause)
		case loopTermination != nil && diagnosticDetailString(loopTermination, "reason_code") == "exploratory_tool_churn":
			toolName := diagnosticDetailString(loopTermination, "tool_name")
			if toolName == "" {
				toolName = "a read-only tool"
			}
			userNarrative = fmt.Sprintf("The framework stopped this direct execution turn because it kept using %s to gather context without taking action.%s", toolName, hostClause)
		case len(attempts) > 1 && firstAttemptSucceeded && guardRetry != nil:
			guardClause := "a post-response guard forced another attempt"
			if guardName != "" {
				guardClause = guardName + " forced another attempt"
			}
			reasonClause := ""
			if retryReason != "" {
				reasonClause = " (" + retryReason + ")"
			}
			routeClause := "The same route was retried."
			if distinctRoutes > 1 {
				routeClause = "The route widened after the retry."
			}
			outcomeClause := fmt.Sprintf("The turn finished %s on %s.", nonEmpty(summary.Status, "unknown"), finalRoute)
			userNarrative = fmt.Sprintf("The first attempt succeeded, but %s%s. %s %s", guardClause, reasonClause, routeClause, outcomeClause)
		case summary.FallbackCount > 0 || distinctRoutes > 1:
			userNarrative = fmt.Sprintf("The initial route was not enough, so the system widened to fallback and finished %s on %s.", nonEmpty(summary.Status, "unknown"), finalRoute)
		case strings.EqualFold(summary.Status, "ok") && len(attempts) <= 1:
			userNarrative = fmt.Sprintf("The turn completed cleanly on %s with no recovery path triggered.", finalRoute)
		default:
			userNarrative = fmt.Sprintf("The turn finished %s on %s after %d attempt(s).%s", nonEmpty(summary.Status, "unknown"), finalRoute, maxDiagnosticInt(len(attempts), summary.InferenceAttempts), hostClause)
		}
	}

	operatorNarrative := summary.OperatorNarrative
	if needsDerivedNarrative(operatorNarrative) {
		parts := []string{
			fmt.Sprintf("status=%s", nonEmpty(summary.Status, "unknown")),
			fmt.Sprintf("attempts=%d", maxDiagnosticInt(len(attempts), summary.InferenceAttempts)),
			fmt.Sprintf("route=%s", finalRoute),
		}
		if firstAttemptSucceeded {
			parts = append(parts, "first_attempt=ok")
		}
		if guardName != "" {
			parts = append(parts, "guard="+guardName)
		}
		if loopTermination != nil {
			if reason := diagnosticDetailString(loopTermination, "reason_code"); reason != "" {
				parts = append(parts, "termination_cause="+reason)
			}
			if streak := diagnosticDetailInt(loopTermination, "exploration_streak"); streak > 0 {
				parts = append(parts, fmt.Sprintf("exploration_streak=%d", streak))
			}
			if toolName := diagnosticDetailString(loopTermination, "tool_name"); toolName != "" {
				parts = append(parts, "blocked_tool="+toolName)
			}
		}
		if retryReason != "" {
			parts = append(parts, "retry_reason="+retryReason)
		}
		if distinctRoutes > 0 {
			parts = append(parts, fmt.Sprintf("distinct_routes=%d", distinctRoutes))
		}
		if summary.FallbackCount > 0 {
			parts = append(parts, fmt.Sprintf("fallbacks=%d", summary.FallbackCount))
		}
		if summary.VerifierRetryCount > 0 || verifierRetry != nil {
			parts = append(parts, fmt.Sprintf("verifier_retries=%d", maxDiagnosticInt(summary.VerifierRetryCount, boolToInt(verifierRetry != nil))))
		}
		if summary.ReplaySuppressionCount > 0 || replaySuppressed != nil {
			parts = append(parts, fmt.Sprintf("replay_suppressions=%d", maxDiagnosticInt(summary.ReplaySuppressionCount, boolToInt(replaySuppressed != nil))))
		}
		if summary.GuardRetryCount > 0 || guardRetry != nil {
			parts = append(parts, fmt.Sprintf("guard_retries=%d", maxDiagnosticInt(summary.GuardRetryCount, boolToInt(guardRetry != nil))))
		}
		if strings.TrimSpace(summary.ResourcePressure) != "" {
			parts = append(parts, "resource_pressure="+summary.ResourcePressure)
		}
		operatorNarrative = strings.Join(parts, "; ")
	}

	summary.UserNarrative = userNarrative
	summary.OperatorNarrative = operatorNarrative
	return summary
}

type diagnosticAttemptInfo struct {
	Route  string
	Status string
}

func diagnosticAttemptsFromEvents(events []TurnDiagnosticEvent) []diagnosticAttemptInfo {
	starts := make([]TurnDiagnosticEvent, 0)
	finishes := make([]TurnDiagnosticEvent, 0)
	for _, ev := range events {
		switch ev.Type {
		case "model_attempt_started":
			starts = append(starts, ev)
		case "model_attempt_finished":
			finishes = append(finishes, ev)
		}
	}
	attempts := make([]diagnosticAttemptInfo, 0, len(starts))
	for i, start := range starts {
		finish := TurnDiagnosticEvent{}
		hasFinish := i < len(finishes)
		if hasFinish {
			finish = finishes[i]
		}
		route := diagnosticRouteFromEvent(start)
		if route == "unknown" && hasFinish {
			route = diagnosticRouteFromEvent(finish)
		}
		status := strings.ToLower(strings.TrimSpace(start.Status))
		if hasFinish && strings.TrimSpace(finish.Status) != "" {
			status = strings.ToLower(strings.TrimSpace(finish.Status))
		}
		if status == "" {
			status = "unknown"
		}
		attempts = append(attempts, diagnosticAttemptInfo{Route: route, Status: status})
	}
	return attempts
}

func diagnosticDistinctRoutes(attempts []diagnosticAttemptInfo) int {
	seen := map[string]struct{}{}
	for _, attempt := range attempts {
		route := strings.TrimSpace(attempt.Route)
		if route == "" || route == "unknown" {
			continue
		}
		seen[route] = struct{}{}
	}
	return len(seen)
}

func latestDiagnosticEvent(events []TurnDiagnosticEvent, eventType string) *TurnDiagnosticEvent {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == eventType {
			ev := events[i]
			return &ev
		}
	}
	return nil
}

func diagnosticRouteFromEvent(ev TurnDiagnosticEvent) string {
	provider := diagnosticDetailString(&ev, "provider")
	model := diagnosticDetailString(&ev, "model")
	switch {
	case provider != "" && model != "":
		return provider + "/" + model
	case model != "":
		return model
	case provider != "":
		return provider
	default:
		return "unknown"
	}
}

func diagnosticFinalRoute(summary TurnDiagnosticSummary, attempts []diagnosticAttemptInfo) string {
	switch {
	case strings.TrimSpace(summary.FinalProvider) != "" && strings.TrimSpace(summary.FinalModel) != "":
		return summary.FinalProvider + "/" + summary.FinalModel
	case strings.TrimSpace(summary.FinalModel) != "":
		return summary.FinalModel
	case strings.TrimSpace(summary.FinalProvider) != "":
		return summary.FinalProvider
	case len(attempts) > 0 && strings.TrimSpace(attempts[len(attempts)-1].Route) != "":
		return attempts[len(attempts)-1].Route
	default:
		return "the selected route"
	}
}

func firstViolationName(guardRetry *TurnDiagnosticEvent, responseFinalized *TurnDiagnosticEvent) string {
	if guardRetry != nil {
		if name := firstStringFromAny(guardRetry.Details["violations"]); name != "" {
			return name
		}
	}
	if responseFinalized != nil {
		if name := firstStringFromAny(responseFinalized.Details["guard_violations"]); name != "" {
			return name
		}
	}
	return ""
}

func diagnosticDetailString(ev *TurnDiagnosticEvent, key string) string {
	if ev == nil || ev.Details == nil {
		return ""
	}
	value, ok := ev.Details[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func diagnosticDetailInt(ev *TurnDiagnosticEvent, key string) int {
	if ev == nil || ev.Details == nil {
		return 0
	}
	value, ok := ev.Details[key]
	if !ok || value == nil {
		return 0
	}
	return diagnosticToInt(value)
}

func firstStringFromAny(value any) string {
	switch v := value.(type) {
	case []string:
		if len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
	case []any:
		if len(v) > 0 {
			return strings.TrimSpace(fmt.Sprint(v[0]))
		}
	case string:
		return strings.TrimSpace(v)
	}
	return ""
}

func needsDerivedNarrative(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, "collecting evidence about request size") ||
		strings.Contains(lower, "turn diagnostics active")
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func maxDiagnosticInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func diagnosticsStatusFromEvents(events []TurnDiagnosticEvent) string {
	for _, ev := range events {
		if ev.Status == "error" {
			return "degraded"
		}
	}
	return "ok"
}
