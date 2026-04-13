package llm

import (
	"unicode"
	"unicode/utf8"
)

// EstimateTokens returns an approximate token count for the given content.
// Uses character-count heuristics adjusted by script type:
//   - ASCII text: ~4 chars per token (English prose)
//   - CJK characters: ~1.5 chars per token (Chinese, Japanese, Korean)
//   - Code-like content: ~3 chars per token (higher token density)
//
// This replaces the naive len(content)/4 byte-count heuristic which breaks
// on multi-byte characters (CJK, emoji) and overestimates for code.
func EstimateTokens(content string) int {
	if content == "" {
		return 0
	}

	runeCount := utf8.RuneCountInString(content)
	if runeCount == 0 {
		return 0
	}

	// Sample the first 512 runes to classify the content.
	var cjkCount, codeCount, asciiCount int
	sampled := 0
	for _, r := range content {
		if sampled >= 512 {
			break
		}
		sampled++

		if isCJK(r) {
			cjkCount++
		} else if isCodeChar(r) {
			codeCount++
		} else if r < 128 {
			asciiCount++
		}
	}

	if sampled == 0 {
		return runeCount / 4
	}

	// Weighted average: compute effective chars-per-token based on content mix.
	cjkRatio := float64(cjkCount) / float64(sampled)
	codeRatio := float64(codeCount) / float64(sampled)

	// Base: 4 chars/token for ASCII prose.
	charsPerToken := 4.0

	// CJK-heavy content: ~1.5 chars/token.
	if cjkRatio > 0.3 {
		charsPerToken = 1.5 + (4.0-1.5)*(1.0-cjkRatio)
	}

	// Code-heavy content: ~3 chars/token (braces, operators, short identifiers).
	if codeRatio > 0.15 {
		charsPerToken = 3.0
	}

	tokens := float64(runeCount) / charsPerToken
	if tokens < 1 {
		return 1
	}
	return int(tokens)
}

// isCJK returns true if the rune is a CJK unified ideograph or common CJK punctuation.
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hiragana, r)
}

// isCodeChar returns true for characters common in source code but rare in prose.
func isCodeChar(r rune) bool {
	switch r {
	case '{', '}', '[', ']', '(', ')', ';', ':', '=', '<', '>', '|', '&', '!', '~', '^', '%':
		return true
	}
	return false
}
