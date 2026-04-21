package llm

import "strings"

func normalizeTaskIntent(intent string) string {
	return strings.TrimSpace(strings.ToLower(intent))
}

func normalizeTaskComplexity(level string) string {
	return strings.TrimSpace(strings.ToLower(level))
}

func canonicalIntentClassForTask(intent string) string {
	switch normalizeTaskIntent(intent) {
	case "conversational", "creative":
		return IntentConversation.String()
	case "code":
		return IntentCoding.String()
	case "task":
		return IntentToolUse.String()
	case "question":
		return IntentExecution.String()
	default:
		return ""
	}
}

func requestIntentClass(req *Request) string {
	if req == nil {
		return ""
	}
	if intentClass := strings.TrimSpace(strings.ToUpper(req.IntentClass)); intentClass != "" {
		return intentClass
	}
	return canonicalIntentClassForTask(req.TaskIntent)
}

func requestRoutingIntent(req *Request) string {
	if req == nil {
		return ""
	}
	return normalizeTaskIntent(req.TaskIntent)
}

func semanticComplexityScore(level string) (float64, bool) {
	switch normalizeTaskComplexity(level) {
	case "simple":
		return 0.15, true
	case "moderate":
		return 0.45, true
	case "complex":
		return 0.78, true
	case "specialist", "expert":
		return 0.92, true
	default:
		return 0, false
	}
}

func requestRoutingComplexity(req *Request) float64 {
	if req == nil {
		return 0.5
	}
	if score, ok := semanticComplexityScore(req.TaskComplexity); ok {
		return score
	}
	return float64(estimateComplexity(req))
}

func requestRoutingComplexitySource(req *Request) string {
	if req == nil {
		return "heuristic_request_shape"
	}
	if _, ok := semanticComplexityScore(req.TaskComplexity); ok {
		return "pipeline_task_complexity"
	}
	return "heuristic_request_shape"
}

func normalizeRoutingWeights(w RoutingWeights) RoutingWeights {
	total := w.Efficacy + w.Cost + w.Availability + w.Locality + w.Confidence + w.Speed
	if total <= 0 {
		return DefaultRoutingWeights()
	}
	return RoutingWeights{
		Efficacy:     w.Efficacy / total,
		Cost:         w.Cost / total,
		Availability: w.Availability / total,
		Locality:     w.Locality / total,
		Confidence:   w.Confidence / total,
		Speed:        w.Speed / total,
	}
}
