package routes

import (
	"encoding/json"
	"strings"
)

func pipelineHealthFromStages(stagesJSON string) map[string]any {
	type traceStage struct {
		Outcome any `json:"outcome"`
	}
	var stages []traceStage
	if err := json.Unmarshal([]byte(stagesJSON), &stages); err != nil || len(stages) == 0 {
		return traceHealth("unknown", "no parseable stage outcomes")
	}
	skipped := 0
	for _, stage := range stages {
		switch normalizeTraceOutcome(stage.Outcome) {
		case "ok", "success", "miss":
			continue
		case "skipped", "fallthrough":
			skipped++
		case "error", "failed", "fail", "timeout":
			return traceHealth("negative", "one or more stages failed")
		default:
			return traceHealth("unknown", "unrecognized stage outcome")
		}
	}
	if skipped > 0 {
		return traceHealth("degraded", "one or more stages skipped")
	}
	return traceHealth("positive", "all recorded stages completed successfully")
}

func traceHealth(aggregate, reason string) map[string]any {
	return map[string]any{
		"aggregate": aggregate,
		"evidence":  "pipeline_trace_stages",
		"reason":    reason,
	}
}

func normalizeTraceOutcome(raw any) string {
	switch value := raw.(type) {
	case string:
		return strings.ToLower(strings.TrimSpace(value))
	case map[string]any:
		if _, ok := value["Error"]; ok {
			return "error"
		}
		if _, ok := value["error"]; ok {
			return "error"
		}
	}
	return ""
}
