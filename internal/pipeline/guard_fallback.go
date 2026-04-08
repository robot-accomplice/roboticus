package pipeline

import "fmt"

// guardFallbackTemplates maps guard names to deterministic fallback messages (Wave 8, #81).
// These provide specific, actionable fallback responses when guard retries are exhausted,
// rather than generic "please rephrase" messages.
var guardFallbackTemplates = map[string]string{
	"empty_response":         "I wasn't able to generate a response to your request. Could you provide more detail about what you need?",
	"repetition":             "I appear to be repeating myself. Let me try a fresh approach — could you rephrase your question?",
	"system_prompt_leak":     "I can't share my system instructions. How else can I help you?",
	"content_classification": "I can't assist with that particular request. Please ask something else.",
	"subagent_claim":         "I wasn't able to complete the delegation I mentioned. Let me try handling this directly instead.",
	"task_deferral":          "I collected some information but didn't take the action you requested. Let me try again with a more direct approach.",
	"internal_jargon":        "My previous response contained technical details that aren't relevant to you. Let me rephrase.",
	"declared_action":        "I described your action without resolving it. Let me properly determine the outcome.",
	"non_repetition_v2":      "I was repeating content from earlier in our conversation. Let me provide fresh information.",
	"execution_truth":        "I made claims about tool execution that weren't accurate. Let me verify what actually happened.",
	"financial_action_truth": "I claimed a financial action that wasn't verified. Let me check the actual transaction status.",
	"action_verification":    "The financial details I mentioned don't match the actual results. Let me correct that.",
	"perspective":            "I was narrating your actions instead of responding directly. Let me answer from my own perspective.",
	"literary_quote_retry":   "I was relying too heavily on quoted material. Let me provide original, direct information instead.",
	"internal_protocol":      "My response contained internal metadata that shouldn't be visible. Let me clean that up.",
	"personality_integrity":  "My response contained identity information that doesn't match who I am. Let me correct that.",
}

// GetFallbackTemplate returns the deterministic fallback message for a guard,
// or empty string if no specific template exists.
func GetFallbackTemplate(guardName string) string {
	return guardFallbackTemplates[guardName]
}

// fallbackResponse generates a continuity-preserving fallback when guard retries
// are exhausted. Per ARCHITECTURE.md principle 4: "Generic fallback messages that
// lose context are architectural defects."
//
// The fallback includes:
// - What the user asked (from session history)
// - Why the agent couldn't complete (guard name + reason)
// - A suggestion for how to proceed
//
// It never includes the rejected content itself (which may contain leaked data).
func fallbackResponse(session *Session, rejected string, guardName string, reason string) *Outcome {
	// Try deterministic template first.
	if tmpl := GetFallbackTemplate(guardName); tmpl != "" {
		return &Outcome{
			SessionID: session.ID,
			Content:   tmpl,
		}
	}

	// Extract the user's last question for context.
	var lastUserMsg string
	for i := len(session.Messages()) - 1; i >= 0; i-- {
		if session.Messages()[i].Role == "user" {
			lastUserMsg = session.Messages()[i].Content
			break
		}
	}

	var content string
	if lastUserMsg != "" {
		content = fmt.Sprintf(
			"I was working on your request but encountered a safety check (%s). "+
				"Could you try rephrasing? Your original question was about: %s",
			guardName, summarizeQuery(lastUserMsg),
		)
	} else {
		content = fmt.Sprintf(
			"I encountered a safety check (%s: %s) and couldn't complete my response. "+
				"Could you try rephrasing your request?",
			guardName, reason,
		)
	}

	return &Outcome{
		SessionID: session.ID,
		Content:   content,
	}
}

// summarizeQuery returns the first 100 characters of a query for context.
func summarizeQuery(q string) string {
	if len(q) <= 100 {
		return q
	}
	return q[:100] + "..."
}
