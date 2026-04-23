package pipeline

import (
	"regexp"
	"strings"
)

var outputShapeDirectivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)[,;]?\s*return only [^.;!?]+`),
	regexp.MustCompile(`(?i)[,;]?\s*reply (?:with )?only [^.;!?]+`),
	regexp.MustCompile(`(?i)[,;]?\s*reply on one line`),
	regexp.MustCompile(`(?i)[,;]?\s*respond on one line`),
	regexp.MustCompile(`(?i)[,;]?\s*answer in one sentence`),
}

func stripOutputShapeDirectives(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	for _, pattern := range outputShapeDirectivePatterns {
		trimmed = pattern.ReplaceAllString(trimmed, "")
	}
	trimmed = regexp.MustCompile(`(?i)\band\s*([.?!])?$`).ReplaceAllString(trimmed, "")
	trimmed = strings.TrimSpace(trimmed)
	trimmed = strings.Trim(trimmed, " ,;:.!?")
	return trimmed
}

func normalizeSemanticSubgoals(goals []string) []string {
	if len(goals) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(goals))
	seen := make(map[string]struct{}, len(goals))
	for _, goal := range goals {
		trimmed := stripOutputShapeDirectives(goal)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(trimmed))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
