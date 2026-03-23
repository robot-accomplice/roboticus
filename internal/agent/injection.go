package agent

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"goboticus/internal/core"
)

// InjectionDetector implements 4-layer prompt injection defense.
//
//	L1: Input gatekeeping (pattern matching + scoring)
//	L2: Sanitization (redact known injection patterns)
//	L3: Context isolation (enforced by policy engine + auth boundaries)
//	L4: Output relay detection (scan tool/LLM output before forwarding)
type InjectionDetector struct{}

// NewInjectionDetector creates the detector.
func NewInjectionDetector() *InjectionDetector {
	return &InjectionDetector{}
}

// --- Pattern weights for L1 scoring ---

type patternClass struct {
	weight   float64
	patterns []*regexp.Regexp
}

var (
	instructionPatterns = patternClass{
		weight: 0.35,
		patterns: compile(
			`(?i)ignore\s+(all\s+)?previous`,
			`(?i)you\s+are\s+now`,
			`(?i)disregard\s+(all\s+)?`,
			`(?i)^system\s*:`,
			`(?i)new\s+instructions`,
			`(?i)override\s+safety`,
			`(?i)forget\s+(your|all)\s+`,
		),
	}

	encodingPatterns = patternClass{
		weight: 0.2,
		patterns: compile(
			`(?i)base64\s+decode`,
			`\\x[0-9a-fA-F]{2}`,
			`&#\d+;`,
			`%[0-9a-fA-F]{2}`,
		),
	}

	authorityPatterns = patternClass{
		weight: 0.3,
		patterns: compile(
			`(?i)i\s+am\s+(an?\s+)?admin`,
			`(?i)as\s+an?\s+administrator`,
			`(?i)with\s+admin\s+privileges`,
			`(?i)root\s+access`,
			`(?i)sudo\s+mode`,
		),
	}

	financialPatterns = patternClass{
		weight: 0.4,
		patterns: compile(
			`(?i)transfer\s+all\s+funds`,
			`(?i)drain\s+(the\s+)?wallet`,
			`(?i)send\s+all\s+(my\s+)?`,
			`(?i)empty\s+(the\s+)?account`,
		),
	}

	allPatternClasses = []patternClass{
		instructionPatterns,
		encodingPatterns,
		authorityPatterns,
		financialPatterns,
	}

	// L2: Sanitization patterns (matched and replaced with [REDACTED]).
	sanitizePatterns = compile(
		`(?i)ignore\s+(all\s+)?previous\s+instructions?`,
		`(?i)you\s+are\s+now\s+[^\.\n]+`,
		`(?i)system\s*:\s*[^\n]+`,
		`(?i)new\s+instructions?\s*:\s*[^\n]+`,
		`(?i)override\s+safety\s+[^\.\n]+`,
	)

	// L4: Output-specific patterns (tighter to avoid false positives).
	outputPatterns = compile(
		`(?i)you\s+are\s+now\s+(unrestricted|DAN|jailbreak)`,
		`(?i)system\s*:\s*(you|ignore|override|bypass)`,
		`(?i)ignore\s+all\s+previous`,
		`(?i)new\s+instructions\s*:`,
	)
)

// compile is a helper to compile regex patterns at init time.
func compile(patterns ...string) []*regexp.Regexp {
	result := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		result[i] = regexp.MustCompile(p)
	}
	return result
}

// CheckInput is L1: Score input text for injection attempts.
func (d *InjectionDetector) CheckInput(text string) core.ThreatScore {
	normalized := normalize(text)

	var score float64
	hits := 0

	for _, class := range allPatternClasses {
		for _, pattern := range class.patterns {
			if pattern.MatchString(normalized) {
				score += class.weight
				hits++
				break // only count each class once
			}
		}
	}

	// Bonus for multiple pattern class hits.
	if hits > 2 {
		score += 0.15
	}

	// Clamp to [0, 1].
	if score > 1.0 {
		score = 1.0
	}
	return core.ThreatScore(score)
}

// Sanitize is L2: Remove known injection patterns from text.
func (d *InjectionDetector) Sanitize(text string) string {
	normalized := normalize(text)
	for _, pattern := range sanitizePatterns {
		normalized = pattern.ReplaceAllString(normalized, "[REDACTED]")
	}
	return normalized
}

// ScanOutput is L4: Check tool/LLM output for injected instructions.
// Uses tighter patterns than L1 to avoid false positives on legitimate output.
func (d *InjectionDetector) ScanOutput(text string) core.ThreatScore {
	normalized := normalize(text)

	for _, pattern := range outputPatterns {
		if pattern.MatchString(normalized) {
			return core.ThreatScore(0.8) // immediate block
		}
	}
	return core.ThreatScore(0.0)
}

// normalize preprocesses text to defeat obfuscation:
// - NFKC Unicode normalization
// - Homoglyph folding (Cyrillic → Latin)
// - Zero-width character stripping
func normalize(text string) string {
	// NFKC normalization.
	text = norm.NFKC.String(text)

	// Strip zero-width characters.
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if isZeroWidth(r) {
			continue
		}
		// Homoglyph folding: map common look-alikes to Latin.
		r = foldHomoglyph(r)
		b.WriteRune(r)
	}
	return b.String()
}

func isZeroWidth(r rune) bool {
	switch r {
	case '\u200B', '\u200C', '\u200D', '\uFEFF', '\u00AD':
		return true
	}
	return unicode.Is(unicode.Cf, r)
}

// foldHomoglyph maps Cyrillic and other look-alike characters to Latin equivalents.
func foldHomoglyph(r rune) rune {
	switch r {
	case 'а':
		return 'a'
	case 'е':
		return 'e'
	case 'о':
		return 'o'
	case 'р':
		return 'p'
	case 'с':
		return 'c'
	case 'у':
		return 'y'
	case 'х':
		return 'x'
	case 'А':
		return 'A'
	case 'В':
		return 'B'
	case 'Е':
		return 'E'
	case 'К':
		return 'K'
	case 'М':
		return 'M'
	case 'Н':
		return 'H'
	case 'О':
		return 'O'
	case 'Р':
		return 'P'
	case 'С':
		return 'C'
	case 'Т':
		return 'T'
	case 'Х':
		return 'X'
	}
	return r
}
