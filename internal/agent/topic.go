package agent

import (
	"fmt"
	"strings"

	"roboticus/internal/llm"
)

type TopicCategory string

const (
	TopicGeneral   TopicCategory = "general"
	TopicTechnical TopicCategory = "technical"
	TopicCreative  TopicCategory = "creative"
	TopicFinancial TopicCategory = "financial"
	TopicSupport   TopicCategory = "support"
	TopicResearch  TopicCategory = "research"
)

type TopicResult struct {
	Primary    TopicCategory
	Secondary  TopicCategory
	Confidence float64
	Keywords   []string
}

func DetectTopic(messages []string) TopicResult {
	if len(messages) == 0 {
		return TopicResult{Primary: TopicGeneral, Confidence: 0.0}
	}
	combined := strings.ToLower(strings.Join(messages, " "))
	scores := map[TopicCategory]int{
		TopicTechnical: countKeywords(combined, []string{"code", "bug", "api", "function", "error", "deploy", "server", "database", "git", "test"}),
		TopicCreative:  countKeywords(combined, []string{"write", "poem", "story", "creative", "design", "art", "music", "compose"}),
		TopicFinancial: countKeywords(combined, []string{"wallet", "transfer", "balance", "payment", "cost", "price", "budget", "revenue"}),
		TopicSupport:   countKeywords(combined, []string{"help", "issue", "problem", "broken", "fix", "support", "how to", "stuck"}),
		TopicResearch:  countKeywords(combined, []string{"research", "analyze", "compare", "study", "data", "report", "survey", "findings"}),
	}

	primary := TopicGeneral
	secondary := TopicGeneral
	bestScore := 0
	secondScore := 0
	for cat, score := range scores {
		if score > bestScore {
			secondary = primary
			secondScore = bestScore
			primary = cat
			bestScore = score
		} else if score > secondScore {
			secondary = cat
			secondScore = score
		}
	}

	confidence := 0.0
	if bestScore > 0 {
		totalWords := len(strings.Fields(combined))
		if totalWords > 0 {
			confidence = float64(bestScore) / float64(totalWords)
			if confidence > 1.0 {
				confidence = 1.0
			}
		}
	}

	var keywords []string
	for _, word := range strings.Fields(combined) {
		if len(keywords) >= 5 {
			break
		}
		if len(word) > 4 {
			keywords = append(keywords, word)
		}
	}

	return TopicResult{Primary: primary, Secondary: secondary, Confidence: confidence, Keywords: keywords}
}

func countKeywords(text string, keywords []string) int {
	count := 0
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			count++
		}
	}
	return count
}

// --- Topic-Aware History Compression (Rust parity) ---

// TopicBlock is a contiguous group of off-topic messages to be summarized.
type TopicBlock struct {
	Tag              string
	Messages         []llm.Message
	FirstUserContent string
}

type conversationChunk struct {
	Messages []llm.Message
}

// PartitionByTopic splits messages into current-topic (kept in full) and
// off-topic blocks (summarized). Matches Rust's partition_by_topic().
//
// Messages without a TopicTag are treated as current-topic.
func PartitionByTopic(messages []llm.Message, currentTopic string) (currentMsgs []llm.Message, offTopicBlocks []TopicBlock) {
	if currentTopic == "" {
		return messages, nil
	}

	var currentBlock *TopicBlock
	for _, chunk := range chunkConversationMessages(messages) {
		tag := chunkTopicTag(chunk.Messages)
		isCurrent := tag == "" || tag == currentTopic

		if isCurrent {
			// Flush any accumulated off-topic block.
			if currentBlock != nil {
				offTopicBlocks = append(offTopicBlocks, *currentBlock)
				currentBlock = nil
			}
			currentMsgs = append(currentMsgs, chunk.Messages...)
		} else {
			// Accumulate into off-topic block.
			if currentBlock == nil || currentBlock.Tag != tag {
				if currentBlock != nil {
					offTopicBlocks = append(offTopicBlocks, *currentBlock)
				}
				currentBlock = &TopicBlock{Tag: tag}
			}
			currentBlock.Messages = append(currentBlock.Messages, chunk.Messages...)
			for _, m := range chunk.Messages {
				if m.Role == "user" && currentBlock.FirstUserContent == "" {
					currentBlock.FirstUserContent = m.Content
				}
			}
		}
	}
	if currentBlock != nil {
		offTopicBlocks = append(offTopicBlocks, *currentBlock)
	}

	return currentMsgs, offTopicBlocks
}

// SummarizeTopicBlock produces a compact summary for an off-topic message block.
// Format matches Rust: [Earlier topic ({tag}, {N} messages): "{snippet}..."]
func SummarizeTopicBlock(block TopicBlock) string {
	snippet := block.FirstUserContent
	if len(snippet) > 80 {
		snippet = snippet[:80]
	}
	if snippet == "" {
		snippet = "(no user message)"
	}
	return fmt.Sprintf("[Earlier topic (%s, %d messages): \"%s...\"]",
		block.Tag, len(block.Messages), snippet)
}

func chunkConversationMessages(messages []llm.Message) []conversationChunk {
	if len(messages) == 0 {
		return nil
	}
	chunks := make([]conversationChunk, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			callIDs := make(map[string]struct{}, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				callIDs[tc.ID] = struct{}{}
			}
			chunk := conversationChunk{Messages: []llm.Message{msg}}
			for j := i + 1; j < len(messages); j++ {
				next := messages[j]
				if next.Role != "tool" {
					break
				}
				if _, ok := callIDs[next.ToolCallID]; !ok {
					break
				}
				chunk.Messages = append(chunk.Messages, next)
				i = j
			}
			chunks = append(chunks, chunk)
			continue
		}
		chunks = append(chunks, conversationChunk{Messages: []llm.Message{msg}})
	}
	return chunks
}

func chunkTopicTag(messages []llm.Message) string {
	for _, msg := range messages {
		if strings.TrimSpace(msg.TopicTag) != "" {
			return msg.TopicTag
		}
	}
	return ""
}
