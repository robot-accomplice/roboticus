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

	"roboticus/internal/llm"
	"roboticus/internal/session"
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

func isShortReferentialExecutionFollowup(content string) bool {
	lower := strings.TrimSpace(strings.ToLower(content))
	if len(lower) > 96 {
		return false
	}
	return hasExecutionVerb(lower) && hasReferentialTarget(lower)
}

func isShortStructuralActionReference(content string) bool {
	lower := strings.TrimSpace(strings.ToLower(content))
	if lower == "" || len(lower) > 160 || strings.Contains(lower, "?") {
		return false
	}
	if isClearlyNegativeContinuation(lower) {
		return false
	}
	return hasExecutionVerb(lower) && referencesPriorAssistantAction(lower)
}

func hasExecutionVerb(lower string) bool {
	for _, verb := range []string{
		"audit", "check", "compare", "continue", "execute", "examine",
		"extract", "inspect", "locate", "open", "parse", "pull",
		"read", "review", "run", "scan", "test", "verify",
	} {
		if lower == verb || strings.Contains(lower, verb+" ") || strings.Contains(lower, " "+verb) {
			return true
		}
	}
	return false
}

func hasReferentialTarget(lower string) bool {
	for _, target := range []string{
		" it", " that", " there", " them", " folder", " directory", " section",
	} {
		if strings.Contains(" "+lower, target) {
			return true
		}
	}
	return false
}

func referencesPriorAssistantAction(lower string) bool {
	for _, ref := range []string{
		"next step", "next steps", "last message", "previous message",
		"what you identified", "what you said", "your last",
	} {
		if strings.Contains(lower, ref) {
			return true
		}
	}
	return false
}

func isShortStateContinuation(content string) bool {
	lower := strings.TrimSpace(strings.ToLower(content))
	if lower == "" || len(lower) > 96 {
		return false
	}
	if len(strings.Fields(lower)) > 6 {
		return false
	}
	if strings.Contains(lower, "?") {
		return false
	}
	if isClearlyNegativeContinuation(lower) {
		return false
	}
	return true
}

func isClearlyNegativeContinuation(lower string) bool {
	trimmed := strings.Trim(lower, " \t\r\n.!;:")
	negative := []string{
		"no",
		"no thanks",
		"not yet",
		"stop",
		"cancel",
		"don't",
		"do not",
		"never mind",
		"nevermind",
	}
	for _, marker := range negative {
		if trimmed == marker || strings.HasPrefix(trimmed, marker+" ") {
			return true
		}
	}
	return false
}

func assistantProposedNextAction(content string) bool {
	return session.DetectPendingActionArtifact(content) != nil
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
	isReferentialExecution := isShortReferentialExecutionFollowup(content)
	isStructuralActionReference := isShortStructuralActionReference(content)

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

	pending := session.PendingActionArtifact()
	hasPendingActionState := pending != nil || assistantProposedNextAction(previousAssistant)
	isStateContinuation := !isSarcasm &&
		!isContradiction &&
		!isFollowup &&
		!isReferentialExecution &&
		!isStructuralActionReference &&
		hasPendingActionState &&
		isShortStateContinuation(content)

	if !isSarcasm && !isContradiction && !isFollowup && !isReferentialExecution && !isStructuralActionReference && !isStateContinuation {
		return content, false
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

	if isReferentialExecution || isStructuralActionReference || isStateContinuation {
		if pending != nil {
			originalTask := lastUserBeforeLastAssistant(session.Messages())
			return fmt.Sprintf(
				"PENDING ACTION CONFIRMED\n"+
					"Confirmed next action: %s\n"+
					"Instruction: execute the confirmed next action now; do not merely restate or re-answer the background task.\n"+
					"Background task: %s\n"+
					"Previous assistant reply excerpt: %s\n"+
					"User confirmation: %s",
				quoteOrUnknown(pending.ProposedAction),
				quoteOrUnknown(originalTask),
				quoteOrUnknown(pending.SourceAssistantExcerpt),
				content,
			), false
		}
		excerpt := truncateChars(previousAssistant, 360)
		return fmt.Sprintf(
			"User follow-up is a state-based continuation or referential execution request. Resolve it against the immediately previous assistant reply before acting. "+
				"If the previous assistant reply proposed a concrete next action, resume that pending action. If the request uses pronouns or references the prior response, resolve the target against the previous assistant reply. "+
				"If the referenced target is a child of an allowed path or an already-retrieved artifact, attempt the relevant tool/parse step instead of asking for separate configuration; the tool/policy result is authoritative.\n"+
				"Previous assistant reply excerpt:\n\"%s\"\n\nUser request:\n%s",
			excerpt, content,
		), false
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

func lastUserBeforeLastAssistant(messages []llm.Message) string {
	lastAssistant := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && strings.TrimSpace(messages[i].Content) != "" {
			lastAssistant = i
			break
		}
	}
	if lastAssistant <= 0 {
		return ""
	}
	for i := lastAssistant - 1; i >= 0; i-- {
		if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			return truncateChars(strings.TrimSpace(messages[i].Content), 360)
		}
	}
	return ""
}

func quoteOrUnknown(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(unknown)"
	}
	return fmt.Sprintf("%q", s)
}
