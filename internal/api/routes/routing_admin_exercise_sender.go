package routes

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
)

func marshalExercisePhaseTimings(t *llm.ExercisePhaseTimings) string {
	if t == nil {
		return ""
	}
	b, err := json.Marshal(t)
	if err != nil {
		return ""
	}
	return string(b)
}

func exercisePhaseTimingsFromStore(ctx context.Context, store *db.Store, turnID string) *llm.ExercisePhaseTimings {
	if store == nil || strings.TrimSpace(turnID) == "" {
		return nil
	}
	row := db.NewRouteQueries(store).GetTurnDiagnostics(ctx, turnID)
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
		return nil
	}
	timings := &llm.ExercisePhaseTimings{
		TotalMs:                totalMs,
		InferenceAttempts:      int(inferenceAttempts),
		ToolCallCount:          int(toolCallCount),
		GuardRetryCount:        int(guardRetryCount),
		VerifierRetryCount:     int(verifierRetryCount),
		ReplaySuppressionCount: int(replaySuppressionCount),
		StageMs:                make(map[string]int64),
	}
	eventsRows, err := db.NewRouteQueries(store).ListTurnDiagnosticEvents(ctx, turnID)
	if err == nil {
		defer func() { _ = eventsRows.Close() }()
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
				return timings
			}
			switch eventType {
			case "model_attempt_finished":
				timings.ModelInferenceMs += durationMs
			case "tool_call_finished":
				timings.ToolExecutionMs += durationMs
			}
		}
	}
	traceRow := db.NewRouteQueries(store).GetTraceByTurnID(ctx, turnID)
	var traceID, traceTurnID, traceChannel, stagesJSON, traceCreatedAt string
	var traceTotalMs int64
	if err := traceRow.Scan(&traceID, &traceTurnID, &traceChannel, &traceTotalMs, &stagesJSON, &traceCreatedAt); err == nil {
		var stages []pipeline.TraceSpan
		if json.Unmarshal([]byte(stagesJSON), &stages) == nil {
			for _, stage := range stages {
				timings.StageMs[strings.ToUpper(stage.Name)] = stage.DurationMs
			}
		}
	}
	timings.FrameworkOverheadMs = timings.TotalMs - timings.ModelInferenceMs - timings.ToolExecutionMs
	if timings.FrameworkOverheadMs < 0 {
		timings.FrameworkOverheadMs = 0
	}
	return timings
}

func pipelineExercisePromptSender(p pipeline.Runner, store *db.Store, agentName string) llm.ModelSender {
	return func(ctx context.Context, model, content string, timeout time.Duration) (llm.PromptDispatch, error) {
		turnID := db.NewID()
		callCtx, cancel := context.WithTimeout(ctx, llm.ExerciseTurnTimeout(timeout))
		defer cancel()
		callCtx = core.WithModelCallTimeout(callCtx, timeout)
		start := time.Now()
		outcome, err := pipeline.RunPipeline(callCtx, p, pipeline.PresetAPI(), pipeline.Input{
			Content:       content,
			TurnID:        turnID,
			AgentID:       "default",
			AgentName:     agentName,
			Platform:      "api",
			ModelOverride: model,
			NoCache:       true,
			NoEscalate:    true,
		})
		latencyMs := time.Since(start).Milliseconds()
		if err != nil {
			return llm.PromptDispatch{
				LatencyMs:    latencyMs,
				TurnID:       turnID,
				PhaseTimings: exercisePhaseTimingsFromStore(ctx, store, turnID),
			}, err
		}
		if outcome.TurnID != "" {
			turnID = outcome.TurnID
		}
		return llm.PromptDispatch{
			ResponseText: outcome.Content,
			LatencyMs:    latencyMs,
			TurnID:       turnID,
			PhaseTimings: exercisePhaseTimingsFromStore(ctx, store, turnID),
		}, nil
	}
}

func pipelineExerciseWarmupSender(p pipeline.Runner, store *db.Store, agentName string) llm.WarmupSender {
	sendPrompt := pipelineExercisePromptSender(p, store, agentName)
	return func(ctx context.Context, model string, timeout time.Duration) llm.WarmupResult {
		dispatch, err := sendPrompt(ctx, model, llm.WarmupPrompt, timeout)
		res := llm.WarmupResult{LatencyMs: dispatch.LatencyMs}
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				res.TimedOut = true
			} else {
				res.Err = err
			}
		}
		return res
	}
}
