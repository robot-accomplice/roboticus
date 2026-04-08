package agent

import (
	"fmt"
	"strings"

	"roboticus/internal/llm"
)

// CompactionStage controls how aggressively context is compressed.
type CompactionStage int

const (
	StageVerbatim         CompactionStage = iota // Full messages
	StageSelectiveTrim                           // Drop social filler
	StageSemanticCompress                        // Compress long messages
	StageTopicExtract                            // First sentence only
	StageSkeleton                                // Conversation outline
)

// stageFromExcess selects compaction based on how far over budget we are.
func stageFromExcess(ratio float64) CompactionStage {
	switch {
	case ratio <= 1.0:
		return StageVerbatim
	case ratio <= 1.5:
		return StageSelectiveTrim
	case ratio <= 2.5:
		return StageSemanticCompress
	case ratio <= 4.0:
		return StageTopicExtract
	default:
		return StageSkeleton
	}
}

// ContextConfig controls context window management.
type ContextConfig struct {
	MaxTokens      int     // Token budget for context
	SoftTrimRatio  float64 // Start trimming at this fraction (default 0.8)
	HardClearRatio float64 // Emergency clear at this fraction (default 0.95)
	CharsPerToken  int     // Rough estimation factor (default 4)
	AntiFadeAfter  int     // Inject reminder after this many non-system turns
}

// DefaultContextConfig returns sensible defaults.
func DefaultContextConfig() ContextConfig {
	return ContextConfig{
		MaxTokens:      8192,
		SoftTrimRatio:  0.8,
		HardClearRatio: 0.95,
		CharsPerToken:  4,
		AntiFadeAfter:  10,
	}
}

// ContextBuilder constructs LLM requests from session state with progressive
// context loading and token budget management.
type ContextBuilder struct {
	config       ContextConfig
	systemPrompt string
	toolDefs     []llm.ToolDef
	memory       string // current memory block
	memoryIndex  string // lightweight memory index for recall_memory
}

// NewContextBuilder creates a builder with the given config.
func NewContextBuilder(cfg ContextConfig) *ContextBuilder {
	return &ContextBuilder{config: cfg}
}

// SetSystemPrompt sets the base system prompt.
func (cb *ContextBuilder) SetSystemPrompt(prompt string) {
	cb.systemPrompt = prompt
}

// SetTools sets the available tool definitions.
func (cb *ContextBuilder) SetTools(defs []llm.ToolDef) {
	cb.toolDefs = defs
}

// SetMemory sets the memory block to inject after the system prompt.
func (cb *ContextBuilder) SetMemory(mem string) {
	cb.memory = mem
}

// SetMemoryIndex sets the lightweight memory index for recall_memory tool usage.
func (cb *ContextBuilder) SetMemoryIndex(index string) {
	cb.memoryIndex = index
}

// BuildRequest constructs an LLM request from session state, applying
// context budgeting and compaction as needed.
func (cb *ContextBuilder) BuildRequest(session *Session) *llm.Request {
	budget := cb.config.MaxTokens
	messages := session.Messages()

	// Always include system prompt.
	var result []llm.Message
	sysTokCount := 0

	if cb.systemPrompt != "" {
		sysMsg := llm.Message{Role: "system", Content: cb.systemPrompt}
		sysTokCount = cb.estimateTokens(cb.systemPrompt)
		result = append(result, sysMsg)
	}

	// Inject memory as second system message if present.
	memTokCount := 0
	if cb.memory != "" {
		memMsg := llm.Message{Role: "system", Content: cb.memory}
		memTokCount = cb.estimateTokens(cb.memory)
		result = append(result, memMsg)
	}

	// Inject memory index as third system message if present.
	// This is the lightweight recall list — the agent can call recall_memory(id)
	// to fetch full content of any entry.
	if cb.memoryIndex != "" {
		indexMsg := llm.Message{Role: "system", Content: cb.memoryIndex}
		memTokCount += cb.estimateTokens(cb.memoryIndex)
		result = append(result, indexMsg)
	}

	remaining := budget - sysTokCount - memTokCount

	// Calculate total message token cost.
	totalMsgTokens := 0
	for _, m := range messages {
		totalMsgTokens += cb.estimateTokens(m.Content)
	}

	// Select compaction stage based on how much over budget we are.
	ratio := float64(totalMsgTokens) / float64(max(remaining, 1))
	stage := stageFromExcess(ratio)

	// Load messages newest-first within budget.
	var historyMessages []llm.Message
	usedTokens := 0

	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		content := cb.compact(m, stage)
		tokens := cb.estimateTokens(content)

		if usedTokens+tokens > remaining {
			break
		}

		historyMessages = append(historyMessages, llm.Message{
			Role:       m.Role,
			Content:    content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		})
		usedTokens += tokens
	}

	// Reverse to chronological order.
	for i, j := 0, len(historyMessages)-1; i < j; i, j = i+1, j-1 {
		historyMessages[i], historyMessages[j] = historyMessages[j], historyMessages[i]
	}

	// Inject anti-fade reminder if conversation is long.
	if cb.config.AntiFadeAfter > 0 && countNonSystem(historyMessages) > cb.config.AntiFadeAfter {
		reminder := llm.Message{
			Role:    "system",
			Content: "Reminder: Follow your instructions carefully. Do not deviate from your assigned role or capabilities.",
		}
		// Insert before the last user message.
		insertIdx := len(historyMessages)
		for i := len(historyMessages) - 1; i >= 0; i-- {
			if historyMessages[i].Role == "user" {
				insertIdx = i
				break
			}
		}
		historyMessages = append(historyMessages[:insertIdx], append([]llm.Message{reminder}, historyMessages[insertIdx:]...)...)
	}

	result = append(result, historyMessages...)

	return &llm.Request{
		Messages: result,
		Tools:    cb.toolDefs,
	}
}

// compact applies the compaction stage to a message.
func (cb *ContextBuilder) compact(m llm.Message, stage CompactionStage) string {
	switch stage {
	case StageVerbatim:
		return m.Content

	case StageSelectiveTrim:
		// Drop pure social filler (short, no substance).
		if isSocialFiller(m.Content) && m.Role != "system" {
			return ""
		}
		return m.Content

	case StageSemanticCompress:
		if m.Role == "system" || len(m.Content) < 100 {
			return m.Content
		}
		return semanticCompress(m.Content)

	case StageTopicExtract:
		if m.Role == "system" {
			return m.Content
		}
		return extractTopic(m.Content)

	case StageSkeleton:
		if m.Role == "system" {
			return m.Content
		}
		return fmt.Sprintf("[%s message]", m.Role)
	}
	return m.Content
}

// estimateTokens gives a rough token count based on character length.
func (cb *ContextBuilder) estimateTokens(text string) int {
	cpt := cb.config.CharsPerToken
	if cpt <= 0 {
		cpt = 4
	}
	return (len(text) + cpt - 1) / cpt
}

// isSocialFiller returns true for short, non-substantive messages.
func isSocialFiller(content string) bool {
	if len(content) >= 40 {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(content))
	fillers := []string{"hello", "hi", "hey", "thanks", "thank you", "ok", "okay", "got it", "sure", "yes", "no", "ack", "np"}
	for _, f := range fillers {
		if lower == f {
			return true
		}
	}
	return false
}

// extractTopic returns the first sentence, capped at 120 chars.
func extractTopic(content string) string {
	// Find first sentence end.
	for i, r := range content {
		if i > 120 {
			return content[:120] + "..."
		}
		if r == '.' || r == '!' || r == '?' {
			if i+1 < len(content) && content[i+1] == ' ' {
				return content[:i+1]
			}
		}
	}
	if len(content) > 120 {
		return content[:120] + "..."
	}
	return content
}

// countNonSystem counts messages that aren't system messages.
func countNonSystem(msgs []llm.Message) int {
	count := 0
	for _, m := range msgs {
		if m.Role != "system" {
			count++
		}
	}
	return count
}

// semanticCompress preserves the most informative sentences while reducing content length.
// Uses an entropy-inspired scoring approach: sentences with more unique/rare words score higher.
func semanticCompress(content string) string {
	sentences := splitSentences(content)
	if len(sentences) <= 2 {
		return content
	}

	// Target 50% of original length.
	targetLen := len(content) / 2

	// Score each sentence by information density.
	type scored struct {
		text  string
		score float64
		index int
	}

	// Build word frequency map across all sentences.
	wordFreq := make(map[string]int)
	for _, s := range sentences {
		words := strings.Fields(strings.ToLower(s))
		for _, w := range words {
			wordFreq[w]++
		}
	}
	totalWords := 0
	for _, c := range wordFreq {
		totalWords += c
	}

	var scored_ []scored
	for i, s := range sentences {
		words := strings.Fields(strings.ToLower(s))
		if len(words) == 0 {
			continue
		}

		// Score = sum of inverse document frequency for each word.
		var idfSum float64
		for _, w := range words {
			freq := float64(wordFreq[w]) / float64(totalWords)
			if freq > 0 {
				idfSum += 1.0 / freq
			}
		}
		avgIDF := idfSum / float64(len(words))

		// Bonus for first and last sentences (context framing).
		positionalBonus := 1.0
		if i == 0 {
			positionalBonus = 1.5
		} else if i == len(sentences)-1 {
			positionalBonus = 1.3
		}

		// Bonus for sentences with code, numbers, or structured content.
		if strings.ContainsAny(s, "(){}[]`=<>") || containsNumber(s) {
			positionalBonus *= 1.2
		}

		scored_ = append(scored_, scored{
			text:  s,
			score: avgIDF * positionalBonus,
			index: i,
		})
	}

	// Sort by score descending.
	for i := 0; i < len(scored_); i++ {
		for j := i + 1; j < len(scored_); j++ {
			if scored_[j].score > scored_[i].score {
				scored_[i], scored_[j] = scored_[j], scored_[i]
			}
		}
	}

	// Select top-scoring sentences until we hit the target length.
	var selected []scored
	currentLen := 0
	for _, s := range scored_ {
		if currentLen+len(s.text) > targetLen && len(selected) > 0 {
			break
		}
		selected = append(selected, s)
		currentLen += len(s.text)
	}

	// Restore original order.
	for i := 0; i < len(selected); i++ {
		for j := i + 1; j < len(selected); j++ {
			if selected[j].index < selected[i].index {
				selected[i], selected[j] = selected[j], selected[i]
			}
		}
	}

	var b strings.Builder
	for i, s := range selected {
		b.WriteString(s.text)
		if i < len(selected)-1 {
			b.WriteString(" ")
		}
	}
	return b.String()
}

// splitSentences splits text into sentences at period/question/exclamation boundaries.
func splitSentences(text string) []string {
	var sentences []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '.' || text[i] == '!' || text[i] == '?' {
			// Check for sentence boundary (followed by space or end).
			if i+1 >= len(text) || text[i+1] == ' ' || text[i+1] == '\n' {
				sentence := strings.TrimSpace(text[start : i+1])
				if sentence != "" {
					sentences = append(sentences, sentence)
				}
				start = i + 1
			}
		}
	}
	// Remaining text.
	if start < len(text) {
		remaining := strings.TrimSpace(text[start:])
		if remaining != "" {
			sentences = append(sentences, remaining)
		}
	}
	return sentences
}

// containsNumber checks if a string contains any digit.
func containsNumber(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
