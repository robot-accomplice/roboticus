package llm

import (
	"context"
	"strings"
	"time"
)

type InferenceObserver interface {
	RecordEvent(eventType, status, operatorSummary, userSummary string, details map[string]any) string
	RecordTimedEvent(eventType, status, operatorSummary, userSummary string, started time.Time, parentEventID string, details map[string]any) string
	SetSummaryField(key string, value any)
	IncrementSummaryCounter(key string, delta int)
}

type inferenceObserverKey struct{}

func WithInferenceObserver(ctx context.Context, observer InferenceObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, inferenceObserverKey{}, observer)
}

func inferenceObserverFromContext(ctx context.Context) InferenceObserver {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(inferenceObserverKey{})
	if v == nil {
		return nil
	}
	obs, _ := v.(InferenceObserver)
	return obs
}

func classifyInferenceError(err error) (string, string) {
	if err == nil {
		return "", "low"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "context deadline exceeded"):
		return "provider_timeout", "high"
	case strings.Contains(msg, "model runner has unexpectedly stopped"):
		return "model_runner_stopped", "high"
	default:
		return "provider_error", "medium"
	}
}

func inferPrimaryDiagnosis(reasonCode string) string {
	switch reasonCode {
	case "provider_timeout", "model_runner_stopped":
		return "local_model_resource_instability"
	default:
		return "inference_path_degraded"
	}
}
