package agent

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
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
	MaxTokens         int     // Token budget for context (used if BudgetTier not set)
	BudgetTier        int     // 0=L0, 1=L1, 2=L2, 3=L3 — overrides MaxTokens when BudgetConfig is set
	BudgetConfig      *core.ContextBudgetConfig // Tier-aware budget config (nil = use MaxTokens)
	SoulMaxContextPct float64 // Personality cap as fraction of budget (default 0.4)
	SoftTrimRatio     float64 // Start trimming at this fraction (default 0.8)
	HardClearRatio    float64 // Emergency clear at this fraction (default 0.95)
	CharsPerToken     int     // Rough estimation factor (default 4)
	AntiFadeAfter     int     // Inject reminder after this many non-system turns
}

// DefaultContextConfig returns sensible defaults.
func DefaultContextConfig() ContextConfig {
	return ContextConfig{
		MaxTokens:         8192,
		BudgetTier:        1, // L1 default
		SoulMaxContextPct: 0.4,
		SoftTrimRatio:     0.8,
		HardClearRatio:    0.95,
		CharsPerToken:     4,
		AntiFadeAfter:     10,
	}
}

// effectiveBudget resolves the token budget from tier config or flat MaxTokens.
func (cc ContextConfig) effectiveBudget() int {
	if cc.BudgetConfig != nil {
		return cc.BudgetConfig.BudgetForTier(cc.BudgetTier)
	}
	return cc.MaxTokens
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
	budget := cb.config.effectiveBudget()
	messages := session.Messages()

	// Always include system prompt.
	var result []llm.Message
	sysTokCount := 0

	if cb.systemPrompt != "" {
		sysTokCount = cb.estimateTokens(cb.systemPrompt)

		// Personality cap: if system prompt exceeds soul_max_pct of budget,
		// expand budget up to L3 max so history/memory aren't starved.
		// Matches Rust: soul_max_context_pct enforcement.
		if cb.config.SoulMaxContextPct > 0 {
			soulCap := int(float64(budget) * cb.config.SoulMaxContextPct)
			if sysTokCount > soulCap && cb.config.BudgetConfig != nil {
				needed := int(float64(sysTokCount) / cb.config.SoulMaxContextPct)
				l3Max := cb.config.BudgetConfig.L3
				if needed > l3Max {
					needed = l3Max
				}
				if needed > budget {
					budget = needed
				}
			}
		}

		result = append(result, llm.Message{Role: "system", Content: cb.systemPrompt})
	}

	// Inject memory (capped at 25% of budget, matching Rust: l0 / 4).
	// Memory is always present — buildAgentContext guarantees at least an
	// orientation block even when retrieval returns empty.
	memTokCount := 0
	memCap := budget / 4
	if cb.memory != "" {
		memTokens := cb.estimateTokens(cb.memory)
		if memTokens > memCap {
			// Truncate memory to fit within cap (rough char-based truncation).
			maxChars := memCap * cb.config.CharsPerToken
			if maxChars < len(cb.memory) {
				cb.memory = cb.memory[:maxChars] + "\n[...memory truncated to fit budget]"
			}
			memTokens = cb.estimateTokens(cb.memory)
		}
		memTokCount = memTokens
		result = append(result, llm.Message{Role: "system", Content: cb.memory})
	}

	// Inject memory index (lightweight recall list for recall_memory tool).
	if cb.memoryIndex != "" {
		indexTokens := cb.estimateTokens(cb.memoryIndex)
		memTokCount += indexTokens
		result = append(result, llm.Message{Role: "system", Content: cb.memoryIndex})
	}

	// Account for tool definitions in the token budget. Each tool adds ~100-200
	// tokens (name, description, parameter schema). Without this, the context
	// builder overfills the budget and the model gets too much history, drowning
	// the system prompt's tool instructions.
	toolTokCount := 0
	for _, td := range cb.toolDefs {
		// Rough estimate: function name + description + JSON schema overhead.
		toolTokCount += cb.estimateTokens(td.Function.Name + td.Function.Description + string(td.Function.Parameters))
	}

	remaining := budget - sysTokCount - memTokCount - toolTokCount

	// Topic-aware compression (Rust parity): partition messages by topic.
	// Off-topic blocks get summarized; current-topic messages kept in full.
	currentTopic := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && messages[i].TopicTag != "" {
			currentTopic = messages[i].TopicTag
			break
		}
	}

	currentTopicMsgs, offTopicBlocks := PartitionByTopic(messages, currentTopic)

	// Inject off-topic summaries as system notes (cheap, ~20 tokens each).
	var topicSummaries []llm.Message
	for _, block := range offTopicBlocks {
		summary := SummarizeTopicBlock(block)
		topicSummaries = append(topicSummaries, llm.Message{
			Role:    "system",
			Content: summary,
		})
	}

	// Use current-topic messages for the main history budget.
	// Off-topic summaries are prepended (small, fixed cost).
	summaryTokens := 0
	for _, s := range topicSummaries {
		summaryTokens += cb.estimateTokens(s.Content)
	}
	remaining -= summaryTokens

	// Calculate total message token cost (current-topic only).
	totalMsgTokens := 0
	for _, m := range currentTopicMsgs {
		totalMsgTokens += cb.estimateTokens(m.Content)
	}

	// Select compaction stage based on how much over budget we are.
	ratio := float64(totalMsgTokens) / float64(max(remaining, 1))
	stage := stageFromExcess(ratio)

	// Load current-topic messages newest-first within budget.
	//
	// CRITICAL INVARIANT (v1.0.6 fix): the LATEST USER MESSAGE must
	// ALWAYS be included, even when the budget is tight. Pre-v1.0.6
	// this loop blindly broke at the first over-budget message, which
	// meant when the system prompt + memory + tool defs ate the entire
	// budget (system prompt ~2200 tok + memory cap ~2048 tok + 45
	// tool defs ~4500 tok = ~8750 tok against an 8192 budget →
	// `remaining = -556`), historyMessages stayed empty AND THE LLM
	// NEVER SAW THE USER'S PROMPT. The agent's response was then
	// "the user has not provided instructions" — exactly the
	// behavioral pattern that surfaced in the v1.0.6 cache-cleared
	// soak run for 6 of 10 scenarios.
	//
	// The fix has two parts:
	//   (a) Identify the index of the latest user message in
	//       currentTopicMsgs so we never break before including it.
	//   (b) When budget runs out partway through history, drop OLDER
	//       messages first while keeping the latest user message —
	//       that's the message the user is actively waiting for a
	//       response to; older history is context that helps but is
	//       not the request.
	var historyMessages []llm.Message
	usedTokens := 0

	latestUserIdx := -1
	for i := len(currentTopicMsgs) - 1; i >= 0; i-- {
		if currentTopicMsgs[i].Role == "user" {
			latestUserIdx = i
			break
		}
	}

	for i := len(currentTopicMsgs) - 1; i >= 0; i-- {
		m := currentTopicMsgs[i]
		content := cb.compact(m, stage)
		tokens := cb.estimateTokens(content)

		// Latest user message: include unconditionally AND verbatim.
		// Two distinct invariants here:
		//   (1) The message survives even if the budget is exhausted
		//       (older history gets dropped instead of the request
		//       the user is actively waiting for).
		//   (2) The message content is NOT compacted, regardless of
		//       compaction stage. Pre-v1.0.6 this was the layered
		//       bug behind the empty-prompt failure: even when the
		//       user message survived the budget loop, `compact()`
		//       at StageSkeleton replaced its content with the
		//       literal string "[user message]" — so the LLM saw
		//       "[user message]" instead of the actual prompt and
		//       responded "the user has not provided instructions."
		//       The user's prompt is the smallest, most important
		//       payload in the whole request; compacting it makes
		//       no sense regardless of pressure.
		if i == latestUserIdx {
			verbatim := m.Content
			tokens = cb.estimateTokens(verbatim)
			if usedTokens+tokens > remaining {
				log.Warn().
					Int("budget", remaining).
					Int("used", usedTokens).
					Int("user_msg_tokens", tokens).
					Int("system_prompt_tokens", sysTokCount).
					Int("memory_tokens", memTokCount).
					Int("tool_def_tokens", toolTokCount).
					Msg("context budget exhausted by system prompt + memory + tool defs; including latest user message anyway (the alternative — dropping it — produces 'no user instructions' replies). Consider reducing system prompt, memory cap, or tool count.")
			}
			historyMessages = append(historyMessages, llm.Message{
				Role:       m.Role,
				Content:    verbatim,
				ToolCalls:  m.ToolCalls,
				ToolCallID: m.ToolCallID,
				Name:       m.Name,
			})
			usedTokens += tokens
			continue
		}

		// Non-latest-user message: subject to budget. Older history
		// gets dropped first when the budget is tight.
		if usedTokens+tokens > remaining {
			continue
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

	// Inject off-topic summaries before current-topic history.
	result = append(result, topicSummaries...)
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
