package agent

import (
	"fmt"
	"regexp"
	"strings"
)

// MaxNormalizationRetries is the maximum number of normalization retries before giving up.
const MaxNormalizationRetries = 2

// NormalizationPattern describes the kind of protocol failure detected in model output.
type NormalizationPattern int

const (
	// NormMalformedToolCall indicates tool call JSON is syntactically malformed.
	NormMalformedToolCall NormalizationPattern = iota + 1
	// NormNarratedToolUse indicates the model narrated what it would do instead of calling a tool.
	NormNarratedToolUse
	// NormEmptyAction indicates the model produced empty or whitespace-only action.
	NormEmptyAction
)

func (p NormalizationPattern) String() string {
	switch p {
	case NormMalformedToolCall:
		return "malformed_tool_call"
	case NormNarratedToolUse:
		return "narrated_tool_use"
	case NormEmptyAction:
		return "empty_action"
	default:
		return "unknown"
	}
}

// Compiled regex patterns (Rust parity: normalization.rs).
var (
	// Matches an "Action:" or "tool_call" keyword.
	reActionOrToolCall = regexp.MustCompile(`(?i)(Action\s*:|tool_call)`)

	// Matches narration patterns where the model describes intent to use a tool
	// rather than actually using it, only when no Action: block is present.
	reNarrated = regexp.MustCompile(`(?i)(I would use|I'll run|let me use the|I should call|I need to invoke)\s+\w+`)

	// Matches "Action:" followed by only optional whitespace (empty action).
	reEmptyAction = regexp.MustCompile(`(?im)Action\s*:\s*$`)
)

// DetectNormalizationFailure inspects content and returns the first NormalizationPattern
// detected, or 0 if the content looks well-formed.
//
// Detection priority:
//  1. EmptyAction   — content is blank, or "Action:" with nothing after it.
//  2. MalformedToolCall — "Action:" / "tool_call" present but JSON is broken.
//  3. NarratedToolUse — model described intent without an "Action:" block.
func DetectNormalizationFailure(content string) NormalizationPattern {
	// 1. Empty / whitespace-only content.
	if strings.TrimSpace(content) == "" {
		return NormEmptyAction
	}

	// 2. "Action:" present but nothing follows it (trailing whitespace only).
	if reEmptyAction.MatchString(content) {
		return NormEmptyAction
	}

	// 3. "Action:" or "tool_call" present → check whether JSON is well-formed.
	if reActionOrToolCall.MatchString(content) {
		if hasBrokenJSON(content) {
			return NormMalformedToolCall
		}
		// Keyword present and JSON looks balanced — not a failure.
		return 0
	}

	// 4. No "Action:" block at all, but the model narrated tool use.
	if reNarrated.MatchString(content) {
		return NormNarratedToolUse
	}

	return 0
}

// BuildNormalizationRetryPrompt builds a system-level retry instruction for the given pattern.
// toolCount is embedded in the NarratedToolUse message so the model knows how many tools
// are available.
func BuildNormalizationRetryPrompt(pattern NormalizationPattern, toolCount int) string {
	switch pattern {
	case NormMalformedToolCall:
		return "Your previous response contained a malformed tool call. " +
			"Please retry using the correct JSON format:\n" +
			"Action: tool_name\n" +
			`Action Input: {"param": "value"}`
	case NormNarratedToolUse:
		return fmt.Sprintf(
			"You described what you would do instead of doing it. "+
				"Use the Action/Action Input format to actually invoke the tool. "+
				"You have %d tools available.", toolCount)
	case NormEmptyAction:
		return "Your previous response was empty. " +
			"Please provide either a direct answer or use a tool via " +
			"Action/Action Input format."
	default:
		return ""
	}
}

// hasBrokenJSON returns true when the content contains a "{" character but the
// curly braces are unbalanced, indicating broken JSON. Also returns true when
// there are no braces at all after an Action:/tool_call keyword.
func hasBrokenJSON(content string) bool {
	open := strings.Count(content, "{")
	close := strings.Count(content, "}")

	if open == 0 {
		// Keyword present but no JSON object at all — malformed.
		return true
	}

	// Unbalanced braces.
	return open != close
}
