package llm

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// CompressionStrategy defines how to compress messages.
type CompressionStrategy int

const (
	StrategyTruncate         CompressionStrategy = iota // Drop oldest messages
	StrategyDropLowRelevance                            // Drop least relevant
)

// PromptCompressor reduces token count before inference.
type PromptCompressor struct {
	strategy CompressionStrategy
}

// NewPromptCompressor creates a compressor with the given strategy.
func NewPromptCompressor(strategy CompressionStrategy) *PromptCompressor {
	return &PromptCompressor{strategy: strategy}
}

// Compress reduces messages to fit within tokenBudget.
func (pc *PromptCompressor) Compress(messages []Message, tokenBudget int) []Message {
	if len(messages) == 0 {
		return nil
	}

	total := estimateMessageTokens(messages)
	if total <= tokenBudget {
		return messages
	}

	switch pc.strategy {
	case StrategyTruncate:
		return pc.truncateOldest(messages, tokenBudget)
	case StrategyDropLowRelevance:
		return pc.truncateOldest(messages, tokenBudget) // same for now, upgradeable
	case StrategyTopicAware:
		return CompressWithTopicAwareness(messages, tokenBudget)
	default:
		return pc.truncateOldest(messages, tokenBudget)
	}
}

func (pc *PromptCompressor) truncateOldest(messages []Message, budget int) []Message {
	// Keep messages from the end until budget is hit.
	var result []Message
	tokens := 0
	for i := len(messages) - 1; i >= 0; i-- {
		msgTokens := EstimateTokens(messages[i].Content)
		if tokens+msgTokens > budget {
			break
		}
		result = append([]Message{messages[i]}, result...)
		tokens += msgTokens
	}
	if len(result) == 0 && len(messages) > 0 {
		result = messages[len(messages)-1:]
	}
	return result
}

func estimateMessageTokens(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		total += EstimateTokens(m.Content)
	}
	return total
}

// ---------- Smart compression (Rust parity: entropy-based scoring) ----------

// stopWords is the canonical stop list matching Rust's compression.rs STOP_WORDS exactly.
// Rust parity: 78 words from is_stop_word() in compression.rs.
var stopWords = map[string]bool{
	// Rust line 1: verbs
	"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "being": true, "have": true, "has": true, "had": true,
	// Rust line 2: modal verbs
	"do": true, "does": true, "did": true, "will": true, "would": true, "could": true,
	"should": true, "may": true, "might": true, "shall": true, "can": true,
	// Rust line 3: prepositions
	"to": true, "of": true, "in": true, "for": true, "on": true, "with": true,
	"at": true, "by": true, "from": true, "as": true, "into": true, "through": true,
	// Rust line 4: more prepositions and conjunctions
	"during": true, "before": true, "after": true, "above": true, "below": true,
	"between": true, "but": true, "and": true, "or": true, "nor": true, "not": true,
	// Rust line 5: quantifiers and determiners
	"so": true, "yet": true, "both": true, "either": true, "neither": true,
	"each": true, "every": true, "all": true, "any": true, "few": true, "more": true,
	// Rust line 6: more determiners and adverbs
	"most": true, "other": true, "some": true, "such": true, "no": true, "only": true,
	"own": true, "same": true, "than": true, "too": true, "very": true,
	// Rust line 7: adverbs and demonstratives
	"just": true, "also": true, "that": true, "this": true, "these": true,
	"those": true, "it": true, "its": true,
}

// isContentWord returns true for alphabetic tokens longer than 3 characters
// that are not stop words.
func isContentWord(token string) bool {
	if len(token) <= 3 {
		return false
	}
	for _, r := range token {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return !stopWords[strings.ToLower(token)]
}

// hasCodePunctuation checks if a token contains (){}=: characters.
func hasCodePunctuation(token string) bool {
	return strings.ContainsAny(token, "(){}=:")
}

// containsDigit checks if a token contains any digit.
func containsDigit(token string) bool {
	for _, r := range token {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// isCapitalized checks if the first rune is uppercase.
func isCapitalized(token string) bool {
	for _, r := range token {
		return unicode.IsUpper(r)
	}
	return false
}

// scoredToken holds a token with its original position and entropy score.
type scoredToken struct {
	token string
	index int
	score float64
}

// scoreToken computes the entropy-based importance score for a single token.
func scoreToken(token string, index, totalTokens int) float64 {
	score := 0.0
	lower := strings.ToLower(token)

	// Base score by token type.
	if isContentWord(token) {
		score += 3.0
	} else if stopWords[lower] {
		score += 0.5
	} else {
		score += 1.5
	}

	// Code punctuation bonus.
	if hasCodePunctuation(token) {
		score += 2.0
	}

	// Capitalised bonus.
	if isCapitalized(token) {
		score += 1.0
	}

	// Contains digit bonus.
	if containsDigit(token) {
		score += 1.5
	}

	// Position bias: first/last 10%.
	if totalTokens > 0 {
		threshold := float64(totalTokens) * 0.1
		if float64(index) < threshold || float64(totalTokens-1-index) < threshold {
			score += 1.0
		}
	}

	// Length-based info density: ln(len).max(0) * 0.5.
	if len(token) > 0 {
		lnLen := math.Log(float64(len(token)))
		if lnLen > 0 {
			score += lnLen * 0.5
		}
	}

	return score
}

// SmartCompress compresses text using entropy-based token scoring.
// targetRatio is clamped to [0.1, 1.0]. Tokens are scored by importance,
// top-N are kept, and original order is restored.
func SmartCompress(text string, targetRatio float64) string {
	// Clamp ratio.
	if targetRatio < 0.1 {
		targetRatio = 0.1
	}
	if targetRatio > 1.0 {
		targetRatio = 1.0
	}

	// Word-based tokenisation (split_whitespace parity).
	tokens := strings.Fields(text)
	if len(tokens) == 0 {
		return ""
	}

	keepCount := int(math.Ceil(float64(len(tokens)) * targetRatio))
	if keepCount >= len(tokens) {
		return text
	}
	if keepCount < 1 {
		keepCount = 1
	}

	// Score every token.
	scored := make([]scoredToken, len(tokens))
	for i, tok := range tokens {
		scored[i] = scoredToken{
			token: tok,
			index: i,
			score: scoreToken(tok, i, len(tokens)),
		}
	}

	// Sort by score descending; keep top-N.
	sort.SliceStable(scored, func(a, b int) bool {
		return scored[a].score > scored[b].score
	})
	kept := scored[:keepCount]

	// Restore original order.
	sort.SliceStable(kept, func(a, b int) bool {
		return kept[a].index < kept[b].index
	})

	parts := make([]string, len(kept))
	for i, s := range kept {
		parts[i] = s.token
	}
	return strings.Join(parts, " ")
}
