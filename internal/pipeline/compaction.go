package pipeline

import (
	"strings"

	"roboticus/internal/llm"
)

// CompactContext implements 5-stage progressive compaction of conversation
// history to fit within a token budget. Matches Rust's compact_text behavior.
//
// Stages:
//  1. Verbatim   - return full history if it fits
//  2. Selective trim - drop old system messages and tool outputs
//  3. Semantic compress - summarize older turns into key points
//  4. Topic extract - reduce to topic sentences only
//  5. Skeleton   - keep only the last 2 turns + system prompt
func CompactContext(messages []llm.Message, tokenBudget int) []llm.Message {
	if len(messages) == 0 || tokenBudget <= 0 {
		return messages
	}

	// Stage 1: Verbatim - return full history if it fits.
	if estimateTokens(messages) <= tokenBudget {
		return messages
	}

	// Stage 2: Selective trim - drop old system messages and tool outputs.
	trimmed := selectiveTrim(messages)
	if estimateTokens(trimmed) <= tokenBudget {
		return trimmed
	}

	// Stage 3: Semantic compress - summarize older turns into key points.
	compressed := semanticCompress(trimmed)
	if estimateTokens(compressed) <= tokenBudget {
		return compressed
	}

	// Stage 4: Topic extract - reduce to topic sentences only.
	extracted := topicExtract(compressed)
	if estimateTokens(extracted) <= tokenBudget {
		return extracted
	}

	// Stage 5: Skeleton - keep only the last 2 turns + system prompt.
	return skeleton(messages)
}

// estimateTokens returns a rough token count using the 4-chars-per-token heuristic.
func estimateTokens(messages []llm.Message) int {
	total := 0
	for _, m := range messages {
		// Each message has overhead (~4 tokens for role + framing).
		total += 4
		total += llm.EstimateTokens(m.Content)
	}
	return total
}

// EstimateMessageTokens is the exported version for testing.
func EstimateMessageTokens(messages []llm.Message) int {
	return estimateTokens(messages)
}

// selectiveTrim removes old system messages and tool result messages,
// keeping only the most recent system message and the last N turns.
func selectiveTrim(messages []llm.Message) []llm.Message {
	var result []llm.Message

	// Find the last system message index.
	lastSystemIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "system" {
			lastSystemIdx = i
			break
		}
	}

	for i, m := range messages {
		switch m.Role {
		case "system":
			// Keep only the most recent system message.
			if i == lastSystemIdx {
				result = append(result, m)
			}
		case "tool":
			// Drop tool results from the first half of conversation.
			if i >= len(messages)/2 {
				result = append(result, m)
			}
		default:
			result = append(result, m)
		}
	}

	return result
}

// semanticCompress summarizes older user/assistant turns into condensed
// key-point messages while keeping the most recent turns verbatim.
func semanticCompress(messages []llm.Message) []llm.Message {
	if len(messages) <= 4 {
		return messages
	}

	// Keep the last 4 messages verbatim. Compress everything before.
	boundary := len(messages) - 4
	var result []llm.Message

	// Compress older messages into summaries.
	var summaryParts []string
	for i := 0; i < boundary; i++ {
		m := messages[i]
		if m.Role == "system" {
			// Preserve system messages as-is.
			result = append(result, m)
			continue
		}
		summary := extractKeyPoints(m.Content)
		if summary != "" {
			prefix := "[" + m.Role + "] "
			summaryParts = append(summaryParts, prefix+summary)
		}
	}

	// Add compressed history as a single system message.
	if len(summaryParts) > 0 {
		result = append(result, llm.Message{
			Role:    "system",
			Content: "[Conversation summary]\n" + strings.Join(summaryParts, "\n"),
		})
	}

	// Append recent messages verbatim.
	result = append(result, messages[boundary:]...)
	return result
}

// topicExtract reduces messages to topic sentences only, keeping structure
// but dropping supporting detail.
func topicExtract(messages []llm.Message) []llm.Message {
	if len(messages) <= 2 {
		return messages
	}

	// Keep last 2 messages verbatim. Extract topics from the rest.
	boundary := len(messages) - 2
	var result []llm.Message

	for i := 0; i < boundary; i++ {
		m := messages[i]
		if m.Role == "system" {
			// Truncate long system prompts.
			if len(m.Content) > 500 {
				result = append(result, llm.Message{
					Role:    m.Role,
					Content: m.Content[:500] + "...",
				})
			} else {
				result = append(result, m)
			}
			continue
		}
		topic := extractTopicSentence(m.Content)
		if topic != "" {
			result = append(result, llm.Message{
				Role:    m.Role,
				Content: topic,
			})
		}
	}

	result = append(result, messages[boundary:]...)
	return result
}

// skeleton keeps only the system prompt and the last 2 user/assistant turns.
func skeleton(messages []llm.Message) []llm.Message {
	var result []llm.Message

	// Keep the first system message (the system prompt).
	for _, m := range messages {
		if m.Role == "system" {
			// Truncate very long system prompts.
			content := m.Content
			if len(content) > 1000 {
				content = content[:1000] + "\n[truncated]"
			}
			result = append(result, llm.Message{Role: "system", Content: content})
			break
		}
	}

	// Keep the last 2 user/assistant message pairs.
	var recentPairs []llm.Message
	for i := len(messages) - 1; i >= 0 && len(recentPairs) < 4; i-- {
		m := messages[i]
		if m.Role == "user" || m.Role == "assistant" {
			recentPairs = append([]llm.Message{m}, recentPairs...)
		}
	}
	result = append(result, recentPairs...)

	return result
}

// extractKeyPoints extracts the most salient points from a message,
// keeping only the first sentence and any sentences containing key markers.
func extractKeyPoints(content string) string {
	sentences := splitSentences(content)
	if len(sentences) == 0 {
		return ""
	}

	var keyPoints []string
	// Always include the first sentence.
	keyPoints = append(keyPoints, sentences[0])

	// Scan for sentences with high-value markers.
	markers := []string{"must", "should", "important", "error", "failed", "success", "result", "conclusion"}
	for i := 1; i < len(sentences); i++ {
		lower := strings.ToLower(sentences[i])
		for _, marker := range markers {
			if strings.Contains(lower, marker) {
				keyPoints = append(keyPoints, sentences[i])
				break
			}
		}
	}

	// Cap at 3 key points.
	if len(keyPoints) > 3 {
		keyPoints = keyPoints[:3]
	}
	return strings.Join(keyPoints, " ")
}

// extractTopicSentence returns only the first sentence of a message.
func extractTopicSentence(content string) string {
	sentences := splitSentences(content)
	if len(sentences) == 0 {
		return ""
	}
	return sentences[0]
}

// splitSentences splits text into sentences using basic punctuation rules.
func splitSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var sentences []string
	var current strings.Builder

	for i, r := range text {
		current.WriteRune(r)
		if (r == '.' || r == '!' || r == '?') && i+1 < len(text) && (text[i+1] == ' ' || text[i+1] == '\n') {
			s := strings.TrimSpace(current.String())
			if s != "" {
				sentences = append(sentences, s)
			}
			current.Reset()
		}
	}

	// Don't lose trailing text without terminal punctuation.
	remainder := strings.TrimSpace(current.String())
	if remainder != "" {
		sentences = append(sentences, remainder)
	}

	return sentences
}
