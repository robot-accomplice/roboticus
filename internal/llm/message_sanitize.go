package llm

import (
	"strings"

	"github.com/rs/zerolog/log"
)

// dropEmptyMessages removes blank messages that carry no structured payload.
//
// Why this exists:
// Upstream context compaction can legitimately decide that a low-value message
// should disappear. When that compaction result is threaded through as an empty
// llm.Message, providers either reject the request or accept it and waste
// context budget on meaningless entries. The live request path should never
// send those ghost messages.
//
// What survives:
// - messages with non-whitespace Content
// - messages with ContentParts
// - messages with ToolCalls
// - messages with ToolCallID (tool result / tool correlation)
//
// Everything else is dropped.
func dropEmptyMessages(messages []Message, caller string) []Message {
	if len(messages) == 0 {
		return messages
	}

	result := make([]Message, 0, len(messages))
	dropped := 0
	for _, msg := range messages {
		if strings.TrimSpace(msg.Content) != "" {
			result = append(result, msg)
			continue
		}
		if len(msg.ContentParts) > 0 || len(msg.ToolCalls) > 0 || msg.ToolCallID != "" {
			result = append(result, msg)
			continue
		}
		dropped++
	}

	if dropped > 0 {
		log.Warn().
			Str("caller", caller).
			Int("dropped_messages", dropped).
			Int("before", len(messages)).
			Int("after", len(result)).
			Msg("dropping empty LLM messages before provider dispatch")
	}

	return result
}
