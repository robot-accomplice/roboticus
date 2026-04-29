package routes

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"roboticus/internal/db"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
)

func contextFootprintForTurn(ctx context.Context, store *db.Store, turnID string) (map[string]any, bool) {
	var stagesJSON string
	err := store.QueryRowContext(ctx,
		`SELECT stages_json FROM pipeline_traces WHERE turn_id = ? ORDER BY created_at DESC LIMIT 1`,
		turnID,
	).Scan(&stagesJSON)
	if err != nil || stagesJSON == "" {
		return nil, false
	}
	var stages []pipeline.TraceSpan
	if err := json.Unmarshal([]byte(stagesJSON), &stages); err != nil {
		return nil, false
	}
	for i := len(stages) - 1; i >= 0; i-- {
		meta := stages[i].Metadata
		if len(meta) == 0 {
			continue
		}
		if _, ok := meta["inference.context_footprint.categories"]; !ok {
			continue
		}
		return normalizeContextFootprint(meta), true
	}
	return nil, false
}

func normalizeContextFootprint(meta map[string]any) map[string]any {
	categories := normalizeTokenMap(meta["inference.context_footprint.categories"])
	budget := numberToInt64(meta["inference.context_footprint.token_budget"])
	used := numberToInt64(meta["inference.context_footprint.used_tokens"])
	unused := numberToInt64(meta["inference.context_footprint.unused_tokens"])
	overhead := numberToInt64(meta["inference.context_footprint.overhead_tokens"])
	if budget <= 0 {
		budget = used + unused
	}
	if used <= 0 {
		for key, val := range categories {
			if key != llm.ContextKindUnused {
				used += val
			}
		}
	}
	if unused <= 0 && budget > used {
		unused = budget - used
	}
	if overhead <= 0 {
		overhead = used - categories[llm.ContextKindCurrentUser]
		if overhead < 0 {
			overhead = 0
		}
	}
	details := normalizeFootprintDetails(meta["inference.context_footprint.details"])
	if len(details[llm.ContextKindTools]) == 0 {
		if selected := normalizeStringList(meta["inference.routing.selected_tools"]); len(selected) > 0 {
			for _, name := range selected {
				details[llm.ContextKindTools] = append(details[llm.ContextKindTools], map[string]any{
					"kind":    "tool",
					"name":    name,
					"tokens":  int64(0),
					"preview": "Selected for this turn; definition token attribution was not captured on this trace span.",
				})
			}
			if _, ok := categories[llm.ContextKindTools]; !ok {
				categories[llm.ContextKindTools] = 0
			}
		}
	}
	segments := footprintSegments(categories, details, budget)
	return map[string]any{
		"token_budget":     budget,
		"used_tokens":      used,
		"unused_tokens":    unused,
		"overhead_tokens":  overhead,
		"overhead_pct":     percentOf(overhead, budget),
		"categories":       categories,
		"category_details": details,
		"segments":         segments,
		"source":           "pipeline_trace",
	}
}

func legacyContextFootprint(budget, system, memoryTokens, history int64) map[string]any {
	categories := map[string]int64{
		llm.ContextKindSystem:  system,
		llm.ContextKindMemory:  memoryTokens,
		llm.ContextKindHistory: history,
	}
	used := system + memoryTokens + history
	unused := budget - used
	if unused < 0 {
		unused = 0
	}
	categories[llm.ContextKindUnused] = unused
	details := map[string][]map[string]any{}
	return map[string]any{
		"token_budget":     budget,
		"used_tokens":      used,
		"unused_tokens":    unused,
		"overhead_tokens":  used,
		"overhead_pct":     percentOf(used, budget),
		"categories":       categories,
		"category_details": details,
		"segments":         footprintSegments(categories, details, budget),
		"source":           "context_snapshot_legacy",
	}
}

func normalizeTokenMap(v any) map[string]int64 {
	out := make(map[string]int64)
	if m, ok := v.(map[string]any); ok {
		for key, val := range m {
			out[key] = numberToInt64(val)
		}
	}
	return out
}

func normalizeFootprintDetails(v any) map[string][]map[string]any {
	out := make(map[string][]map[string]any)
	raw, ok := v.(map[string]any)
	if !ok {
		return out
	}
	for key, listAny := range raw {
		list, ok := listAny.([]any)
		if !ok {
			continue
		}
		for _, item := range list {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			detail := map[string]any{
				"kind":    stringValue(itemMap["kind"]),
				"role":    stringValue(itemMap["role"]),
				"name":    stringValue(itemMap["name"]),
				"tokens":  numberToInt64(itemMap["tokens"]),
				"preview": stringValue(itemMap["preview"]),
			}
			out[key] = append(out[key], detail)
		}
	}
	return out
}

func normalizeStringList(v any) []string {
	var out []string
	switch list := v.(type) {
	case []string:
		for _, item := range list {
			if strings.TrimSpace(item) != "" {
				out = append(out, item)
			}
		}
	case []any:
		for _, item := range list {
			if s := strings.TrimSpace(stringValue(item)); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func footprintSegments(categories map[string]int64, details map[string][]map[string]any, budget int64) []map[string]any {
	order := []string{
		llm.ContextKindSystem,
		llm.ContextKindTools,
		llm.ContextKindMemory,
		llm.ContextKindMemoryIndex,
		llm.ContextKindAmbient,
		llm.ContextKindTopicSummary,
		llm.ContextKindHistory,
		llm.ContextKindCurrentUser,
		llm.ContextKindExecutionOverlay,
		llm.ContextKindReflection,
		llm.ContextKindUnused,
	}
	seen := make(map[string]bool, len(order))
	segments := make([]map[string]any, 0, len(categories))
	for _, key := range order {
		seen[key] = true
		tokens := categories[key]
		if tokens <= 0 && len(details[key]) == 0 {
			continue
		}
		segments = append(segments, map[string]any{
			"key":     key,
			"label":   contextFootprintLabel(key),
			"tokens":  tokens,
			"pct":     percentOf(tokens, budget),
			"details": details[key],
		})
	}
	var extra []string
	for key := range categories {
		if !seen[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	for _, key := range extra {
		tokens := categories[key]
		segments = append(segments, map[string]any{
			"key":     key,
			"label":   contextFootprintLabel(key),
			"tokens":  tokens,
			"pct":     percentOf(tokens, budget),
			"details": details[key],
		})
	}
	return segments
}

func contextFootprintLabel(key string) string {
	switch key {
	case llm.ContextKindSystem:
		return "System"
	case llm.ContextKindTools:
		return "Tools"
	case llm.ContextKindMemory:
		return "Memory"
	case llm.ContextKindMemoryIndex:
		return "Memory Index"
	case llm.ContextKindAmbient:
		return "Runtime Notes"
	case llm.ContextKindTopicSummary:
		return "Topic Summaries"
	case llm.ContextKindHistory:
		return "History"
	case llm.ContextKindCurrentUser:
		return "Latest User"
	case llm.ContextKindExecutionOverlay:
		return "Execution Overlay"
	case llm.ContextKindReflection:
		return "Reflection"
	case llm.ContextKindUnused:
		return "Unused"
	default:
		return key
	}
}

func numberToInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func percentOf(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total)
}
