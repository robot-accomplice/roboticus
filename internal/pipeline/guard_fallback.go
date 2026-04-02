package pipeline

import "fmt"

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
