package pipeline

import (
	"strings"

	"roboticus/internal/llm"
)

type compactionChunk struct {
	start    int
	messages []llm.Message
}

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
	chunks := chunkCompactionMessages(messages)

	// Find the last system message index.
	lastSystemIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "system" {
			lastSystemIdx = i
			break
		}
	}

	for _, chunk := range chunks {
		if len(chunk.messages) == 0 {
			continue
		}
		first := chunk.messages[0]
		switch {
		case first.Role == "system":
			// Keep only the most recent system message.
			if chunk.start == lastSystemIdx {
				result = append(result, chunk.messages...)
			}
		case isToolCallChunk(chunk):
			// Drop entire old tool exchanges together. Keeping the assistant
			// tool_calls while dropping matching tool results corrupts provider
			// history on later inference/retry paths.
			if chunk.start >= len(messages)/2 {
				result = append(result, chunk.messages...)
			}
		default:
			result = append(result, chunk.messages...)
		}
	}

	return result
}

// semanticCompress summarizes older user/assistant turns into condensed
// key-point messages while keeping the most recent turns verbatim.
func semanticCompress(messages []llm.Message) []llm.Message {
	chunks := chunkCompactionMessages(messages)
	if len(chunks) <= 4 {
		return messages
	}

	// Keep the last 4 chunks verbatim. Compress everything before.
	boundary := len(chunks) - 4
	var result []llm.Message

	// Compress older messages into summaries.
	var summaryParts []string
	for i := 0; i < boundary; i++ {
		chunk := chunks[i]
		if isSystemChunk(chunk) {
			// Preserve system messages as-is.
			result = append(result, chunk.messages...)
			continue
		}
		summary := summarizeCompactionChunk(chunk)
		if summary != "" {
			prefix := "[" + chunk.messages[0].Role + "] "
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
	for _, chunk := range chunks[boundary:] {
		result = append(result, chunk.messages...)
	}
	return result
}

// topicExtract reduces messages to topic sentences only, keeping structure
// but dropping supporting detail.
func topicExtract(messages []llm.Message) []llm.Message {
	chunks := chunkCompactionMessages(messages)
	if len(chunks) <= 2 {
		return messages
	}

	// Keep last 2 chunks verbatim. Extract topics from the rest.
	boundary := len(chunks) - 2
	var result []llm.Message

	for i := 0; i < boundary; i++ {
		chunk := chunks[i]
		if isSystemChunk(chunk) {
			// Truncate long system prompts.
			content := chunk.messages[0].Content
			if len(content) > 500 {
				result = append(result, llm.Message{
					Role:    chunk.messages[0].Role,
					Content: content[:500] + "...",
				})
			} else {
				result = append(result, chunk.messages...)
			}
			continue
		}
		topic := summarizeCompactionChunk(chunk)
		if topic != "" {
			result = append(result, llm.Message{
				Role:    chunk.messages[0].Role,
				Content: topic,
			})
		}
	}

	for _, chunk := range chunks[boundary:] {
		result = append(result, chunk.messages...)
	}
	return result
}

// skeleton keeps only the system prompt and the last 2 user/assistant turns.
func skeleton(messages []llm.Message) []llm.Message {
	var result []llm.Message
	chunks := chunkCompactionMessages(messages)

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

	// Keep the last 2 non-system chunks verbatim. A tool-call exchange is one
	// atomic chunk, not permission to keep only the assistant tool_calls.
	var recentChunks [][]llm.Message
	for i := len(chunks) - 1; i >= 0 && len(recentChunks) < 2; i-- {
		chunk := chunks[i]
		if isSystemChunk(chunk) {
			continue
		}
		recentChunks = append([][]llm.Message{chunk.messages}, recentChunks...)
	}
	for _, chunk := range recentChunks {
		result = append(result, chunk...)
	}

	return result
}

func chunkCompactionMessages(messages []llm.Message) []compactionChunk {
	if len(messages) == 0 {
		return nil
	}
	chunks := make([]compactionChunk, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			callIDs := make(map[string]struct{}, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				callIDs[tc.ID] = struct{}{}
			}
			chunk := compactionChunk{start: i, messages: []llm.Message{msg}}
			for j := i + 1; j < len(messages); j++ {
				next := messages[j]
				if next.Role != "tool" {
					break
				}
				if _, ok := callIDs[next.ToolCallID]; !ok {
					break
				}
				chunk.messages = append(chunk.messages, next)
				i = j
			}
			chunks = append(chunks, chunk)
			continue
		}
		chunks = append(chunks, compactionChunk{start: i, messages: []llm.Message{msg}})
	}
	return chunks
}

func isToolCallChunk(chunk compactionChunk) bool {
	return len(chunk.messages) > 0 && chunk.messages[0].Role == "assistant" && len(chunk.messages[0].ToolCalls) > 0
}

func isSystemChunk(chunk compactionChunk) bool {
	return len(chunk.messages) == 1 && chunk.messages[0].Role == "system"
}

func summarizeCompactionChunk(chunk compactionChunk) string {
	if len(chunk.messages) == 0 {
		return ""
	}
	if isToolCallChunk(chunk) {
		names := make([]string, 0, len(chunk.messages[0].ToolCalls))
		for _, tc := range chunk.messages[0].ToolCalls {
			if strings.TrimSpace(tc.Function.Name) == "" {
				continue
			}
			names = append(names, tc.Function.Name)
		}
		if len(names) > 0 {
			return "Tool exchange: " + strings.Join(names, ", ")
		}
	}
	for _, msg := range chunk.messages {
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		return extractTopicSentence(msg.Content)
	}
	return ""
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
