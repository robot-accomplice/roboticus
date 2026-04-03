package pipeline

import (
	"regexp"
	"strings"
)

// NormalizationPattern identifies the type of malformed tool output.
type NormalizationPattern int

const (
	PatternNone NormalizationPattern = iota
	PatternMalformedToolCall
	PatternNarratedToolUse
	PatternEmptyAction
)

func (p NormalizationPattern) String() string {
	switch p {
	case PatternMalformedToolCall:
		return "malformed_tool_call"
	case PatternNarratedToolUse:
		return "narrated_tool_use"
	case PatternEmptyAction:
		return "empty_action"
	default:
		return "none"
	}
}

// MaxNormalizationRetries is the maximum number of retry attempts for
// malformed tool output before giving up.
const MaxNormalizationRetries = 2

var (
	emptyActionRe  = regexp.MustCompile(`(?i)Action\s*:\s*$`)
	narratedToolRe = regexp.MustCompile(`(?i)(I would use|I'll run|let me use the|I should call|I need to invoke)\s+\w+`)
)

// DetectNormalizationIssue checks if a response has a malformed tool pattern.
func DetectNormalizationIssue(content string) NormalizationPattern {
	trimmed := strings.TrimSpace(content)

	// Priority 1: Empty action.
	if trimmed == "" || emptyActionRe.MatchString(trimmed) {
		return PatternEmptyAction
	}

	// Priority 2: Malformed tool call (broken JSON after Action: keyword).
	if strings.Contains(trimmed, "Action:") || strings.Contains(trimmed, "tool_call") {
		if hasBrokenJSON(trimmed) {
			return PatternMalformedToolCall
		}
	}

	// Priority 3: Narrated tool use without Action: block.
	if !strings.Contains(trimmed, "Action:") && narratedToolRe.MatchString(trimmed) {
		return PatternNarratedToolUse
	}

	return PatternNone
}

// NormalizationRetryPrompt generates a corrective prompt for the detected pattern.
func NormalizationRetryPrompt(pattern NormalizationPattern, toolCount int) string {
	switch pattern {
	case PatternMalformedToolCall:
		return "Your previous response contained a malformed tool call. " +
			"Please retry using the correct JSON format:\n" +
			"Action: tool_name\n" +
			`Action Input: {"param": "value"}`
	case PatternNarratedToolUse:
		return "You described what you would do instead of doing it. " +
			"Use the Action/Action Input format to actually invoke the tool. " +
			"You have " + intToStr(toolCount) + " tools available."
	case PatternEmptyAction:
		return "Your previous response was empty. " +
			"Please provide either a direct answer or use a tool via " +
			"Action/Action Input format."
	default:
		return ""
	}
}

// hasBrokenJSON checks for unbalanced braces in the content.
func hasBrokenJSON(content string) bool {
	open := strings.Count(content, "{")
	close := strings.Count(content, "}")
	return open == 0 || open != close
}

func intToStr(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	// Simple fallback for larger numbers.
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if s == "" {
		return "0"
	}
	return s
}
