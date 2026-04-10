// Short-followup expansion heuristics.
//
// Detects brief user reactions (sarcasm, contradiction, quote-back) and
// prepends context from the previous assistant reply so the LLM can
// understand the reference. Also drives is_correction_turn so that
// shortcut dispatch is skipped for corrections.
//
// Ported from Rust: crates/roboticus-pipeline/src/heuristics.rs

package pipeline

import (
	"fmt"
	"strings"
)

// isShortFollowupForPreviousReply detects brief messages that reference the
// previous assistant reply (e.g., "what's that from?", "source?").
func isShortFollowupForPreviousReply(content string) bool {
	lower := strings.TrimSpace(strings.ToLower(content))
	if len(lower) > 80 {
		return false
	}
	markers := []string{
		"what's that from",
		"what is that from",
		"where is that from",
		"no, your quote",
		"your quote",
		"what quote",
		"source?",
	}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// isShortReactiveSarcasm detects brief sarcastic reactions (≤32 chars).
// Must be exact match or with trailing period/ellipsis.
func isShortReactiveSarcasm(content string) bool {
	lower := strings.TrimSpace(strings.ToLower(content))
	if len(lower) > 32 {
		return false
	}
	markers := []string{
		"wow", "great", "fantastic", "amazing",
		"incredible", "brilliant", "sure", "right",
	}
	for _, m := range markers {
		if lower == m || lower == m+"." || lower == m+"..." {
			return true
		}
	}
	return false
}

// isShortContradictionFollowup detects brief contradictions (≤48 chars).
func isShortContradictionFollowup(content string) bool {
	lower := strings.TrimSpace(strings.ToLower(content))
	if len(lower) > 48 {
		return false
	}
	markers := []string{
		"that's not true",
		"that is not true",
		"not true",
		"that's wrong",
		"that is wrong",
		"incorrect",
	}
	for _, m := range markers {
		if lower == m || lower == m+"." || strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// ContextualizeShortFollowup expands a short user reaction by prepending the
// previous assistant reply for context. Returns (expanded_content, is_correction_turn).
//
// When sarcasm or contradiction is detected, the expanded content includes
// coaching instructions for the LLM to handle the reaction appropriately.
func ContextualizeShortFollowup(session *Session, content string) (string, bool) {
	isSarcasm := isShortReactiveSarcasm(content)
	isContradiction := isShortContradictionFollowup(content)
	isFollowup := isShortFollowupForPreviousReply(content)

	if !isSarcasm && !isContradiction && !isFollowup {
		return content, false
	}

	// Find last assistant message.
	msgs := session.Messages()
	var previousAssistant string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && strings.TrimSpace(msgs[i].Content) != "" {
			previousAssistant = strings.TrimSpace(msgs[i].Content)
			break
		}
	}

	if previousAssistant == "" {
		return content, isSarcasm || isContradiction
	}

	correction := isSarcasm || isContradiction

	if isSarcasm {
		excerpt := truncateChars(previousAssistant, 240)
		return fmt.Sprintf(
			"User likely reacted with sarcasm/frustration to your previous reply. "+
				"Acknowledge the miss directly, do not treat it as praise, and correct course.\n"+
				"Previous assistant reply excerpt:\n\"%s\"\n\nUser reaction:\n%s",
			excerpt, content,
		), correction
	}

	if isContradiction {
		excerpt := truncateChars(previousAssistant, 240)
		return fmt.Sprintf(
			"User directly disputed your previous reply as incorrect. "+
				"Acknowledge the error and provide a corrected answer grounded in available tools/delegation.\n"+
				"Previous assistant reply excerpt:\n\"%s\"\n\nUser follow-up:\n%s",
			excerpt, content,
		), correction
	}

	// Quote-back / source followup.
	quote := truncateChars(previousAssistant, 360)
	return fmt.Sprintf(
		"User follow-up references your immediately previous reply. "+
			"Answer specifically what that prior reply/quote is from.\n"+
			"Previous assistant reply excerpt:\n\"%s\"\n\nUser question:\n%s",
		quote, content,
	), false
}

// truncateChars truncates a string to at most maxChars characters.
func truncateChars(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return string(runes[:maxChars])
}
