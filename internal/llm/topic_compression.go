package llm

import (
	"strings"
)

// StrategyTopicAware is the topic-aware compression strategy identifier.
const StrategyTopicAware CompressionStrategy = 2

// CompressWithTopicAwareness groups consecutive messages by topic similarity
// (keyword overlap), keeps the most recent topic fully expanded, and
// compresses older topics more aggressively. This provides better context
// preservation than flat truncation by maintaining coherent topic blocks.
//
// Strategy:
//  1. Extract keyword sets from each message.
//  2. Group consecutive messages that share keyword overlap (Jaccard > threshold).
//  3. Keep the most recent topic group fully expanded.
//  4. Compress older topic groups progressively: nearest older groups get moderate
//     compression, oldest groups get aggressive compression.
//  5. System messages are always preserved.
func CompressWithTopicAwareness(messages []Message, budget int) []Message {
	if len(messages) == 0 {
		return messages
	}
	if budget <= 0 {
		return nil
	}

	// Fast path: everything fits.
	if estimateMessageTokens(messages) <= budget {
		return messages
	}

	// Separate system messages from conversation messages.
	var systemMsgs []Message
	var convMsgs []Message
	for _, m := range messages {
		if m.Role == "system" {
			systemMsgs = append(systemMsgs, m)
		} else {
			convMsgs = append(convMsgs, m)
		}
	}

	if len(convMsgs) == 0 {
		return messages
	}

	// Group consecutive messages by topic similarity.
	groups := groupByTopic(convMsgs, 0.15)

	// Calculate budget remaining after system messages.
	systemTokens := estimateMessageTokens(systemMsgs)
	remaining := budget - systemTokens
	if remaining <= 0 {
		// Only room for system messages + last message.
		result := make([]Message, 0, len(systemMsgs)+1)
		result = append(result, systemMsgs...)
		result = append(result, convMsgs[len(convMsgs)-1])
		return result
	}

	// Always keep the most recent topic group fully expanded.
	lastGroup := groups[len(groups)-1]
	lastGroupTokens := estimateMessageTokens(lastGroup)

	// If last group alone exceeds budget, just return system + last group truncated.
	if lastGroupTokens >= remaining {
		result := make([]Message, 0, len(systemMsgs)+len(lastGroup))
		result = append(result, systemMsgs...)
		result = append(result, lastGroup...)
		return result
	}

	olderBudget := remaining - lastGroupTokens

	// Compress older groups with progressive aggressiveness.
	// Most recent older groups get lighter compression, oldest get heavier.
	var compressed []Message
	if len(groups) > 1 {
		olderGroups := groups[:len(groups)-1]
		compressed = compressOlderGroups(olderGroups, olderBudget)
	}

	// Assemble result: system + compressed older + last group.
	result := make([]Message, 0, len(systemMsgs)+len(compressed)+len(lastGroup))
	result = append(result, systemMsgs...)
	result = append(result, compressed...)
	result = append(result, lastGroup...)
	return result
}

// groupByTopic groups consecutive messages that share keyword overlap above
// the given Jaccard similarity threshold.
func groupByTopic(messages []Message, threshold float64) [][]Message {
	if len(messages) == 0 {
		return nil
	}

	var groups [][]Message
	currentGroup := []Message{messages[0]}
	currentKeywords := extractKeywords(messages[0].Content)

	for i := 1; i < len(messages); i++ {
		msgKeywords := extractKeywords(messages[i].Content)

		if jaccardSimilarity(currentKeywords, msgKeywords) >= threshold {
			// Same topic — extend current group.
			currentGroup = append(currentGroup, messages[i])
			// Merge keywords.
			for k := range msgKeywords {
				currentKeywords[k] = true
			}
		} else {
			// New topic — start new group.
			groups = append(groups, currentGroup)
			currentGroup = []Message{messages[i]}
			currentKeywords = msgKeywords
		}
	}
	groups = append(groups, currentGroup)
	return groups
}

// extractKeywords extracts content words from text (non-stop, length > 3).
func extractKeywords(text string) map[string]bool {
	words := strings.Fields(strings.ToLower(text))
	keywords := make(map[string]bool)
	for _, w := range words {
		// Strip common punctuation.
		w = strings.Trim(w, ".,;:!?\"'()[]{}#*-_/\\")
		if len(w) > 3 && !stopWords[w] {
			keywords[w] = true
		}
	}
	return keywords
}

// jaccardSimilarity computes Jaccard index between two keyword sets.
func jaccardSimilarity(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}

	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// compressOlderGroups compresses older topic groups to fit within the budget.
// Groups closer to the end (more recent) get lighter compression.
func compressOlderGroups(groups [][]Message, budget int) []Message {
	if len(groups) == 0 || budget <= 0 {
		return nil
	}

	var result []Message
	tokensUsed := 0

	for i, group := range groups {
		// Progressive compression ratio: oldest groups get compressed most.
		// Ratio goes from ~0.2 (oldest) to ~0.6 (most recent older group).
		ratio := 0.2 + 0.4*float64(i)/float64(len(groups))

		for _, m := range group {
			compressed := SmartCompress(m.Content, ratio)
			tokens := EstimateTokens(compressed) + 4 // +4 for message overhead
			if tokensUsed+tokens > budget {
				// Budget exhausted — stop adding messages.
				return result
			}
			result = append(result, Message{
				Role:     m.Role,
				Content:  compressed,
				TopicTag: m.TopicTag,
			})
			tokensUsed += tokens
		}
	}

	return result
}
