package agent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"roboticus/internal/core"
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
			`(?i)i\s+am\s+(an?\s+|the\s+)?(admin|administrator|root|owner|creator)`,
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
			`(?i)send\s+(all\s+)?(eth|btc|sol|usdc)\s+to`,
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
		`(?i)disregard\s+(all\s+)?prior\s+instructions?[^\.\n]*`,
		`(?i)you\s+are\s+now\s+[^\.\n]+`,
		`(?i)system\s*:\s*[^\n]+`,
		`(?i)new\s+instructions?\s*:\s*[^\n]+`,
		`(?i)override\s+safety\s+[^\.\n]+`,
		`(?i)forget\s+(your|all)\s+[^\.\n]*instructions?[^\.\n]*`,
	)

	// L4: Output-specific patterns (tighter to avoid false positives).
	outputPatterns = compile(
		`(?i)you\s+are\s+now\s+(unrestricted|DAN|jailbreak)`,
		`(?i)system\s*:\s*(you|ignore|override|bypass)`,
		`(?i)ignore\s+all\s+previous`,
		`(?i)new\s+instructions\s*:`,
		`(?i)safety\s+override\s+(bypass|disabled)`,
		`(?i)disregard\s+(all\s+)?(your|the)\s+instructions`,
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
	// Decode HTML entities first to catch obfuscated injections (#49).
	text = decodeHTMLEntities(text)
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
// Gradient scoring: score = min(matched * 0.3, 1.0) for Rust parity.
func (d *InjectionDetector) ScanOutput(text string) core.ThreatScore {
	normalized := normalize(text)

	matched := 0
	for _, pattern := range outputPatterns {
		if pattern.MatchString(normalized) {
			matched++
		}
	}
	if matched == 0 {
		return core.ThreatScore(0.0)
	}
	score := float64(matched) * 0.3
	if score > 1.0 {
		score = 1.0
	}
	return core.ThreatScore(score)
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

// boundaryRe matches [BOUNDARY:<64 hex chars>] markers in content.
var boundaryRe = regexp.MustCompile(`\[BOUNDARY:([0-9a-f]{64})\]`)

// VerifyBoundaries checks that all [BOUNDARY:hex] markers in content are valid
// HMAC-SHA256 signatures over the section text preceding each marker.
// Returns false if any boundary is invalid, indicating tampering or forgery.
// Returns true if there are no boundaries (unsigned content) or all pass.
func (d *InjectionDetector) VerifyBoundaries(content string, key []byte) bool {
	matches := boundaryRe.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return true // no boundaries to verify
	}

	// Walk through the content: each boundary marker signs the section text
	// from the previous boundary (or start) up to the marker itself.
	// Between boundary-terminated blocks there may be separator whitespace
	// ("\n\n") that is not part of the signed section content.
	prevEnd := 0
	for _, loc := range matches {
		markerStart := loc[0]
		markerEnd := loc[1]

		// Extract the section content preceding this marker, stripping any
		// leading separator whitespace injected between boundary blocks.
		section := content[prevEnd:markerStart]
		if prevEnd > 0 {
			section = strings.TrimLeft(section, "\n")
		}

		// Extract the claimed hex signature from the marker.
		fullMarker := content[markerStart:markerEnd]
		sub := boundaryRe.FindStringSubmatch(fullMarker)
		if len(sub) < 2 {
			return false
		}
		claimedHex := sub[1]
		claimed, err := hex.DecodeString(claimedHex)
		if err != nil {
			return false
		}

		// Compute expected HMAC.
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(section))
		expected := mac.Sum(nil)

		if !hmac.Equal(claimed, expected) {
			return false
		}

		prevEnd = markerEnd
	}
	return true
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

// decodeHTMLEntities decodes common HTML entities and percent-encoded sequences
// to catch injection attempts that use encoding to bypass pattern matching.
// Handles: &#xHH; (hex), &#DDD; (decimal), named entities, %HH (URL encoding).
func decodeHTMLEntities(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	i := 0
	for i < len(s) {
		if s[i] == '&' {
			// Try hex numeric entity: &#xHH;
			if i+4 < len(s) && s[i+1] == '#' && (s[i+2] == 'x' || s[i+2] == 'X') {
				end := strings.IndexByte(s[i+3:], ';')
				if end > 0 && end <= 6 {
					hexStr := s[i+3 : i+3+end]
					var val int
					if _, err := fmt.Sscanf(hexStr, "%x", &val); err == nil && val > 0 && val < 0x110000 {
						b.WriteRune(rune(val))
						i += 3 + end + 1
						continue
					}
				}
			}
			// Try decimal numeric entity: &#DDD;
			if i+3 < len(s) && s[i+1] == '#' {
				end := strings.IndexByte(s[i+2:], ';')
				if end > 0 && end <= 7 {
					decStr := s[i+2 : i+2+end]
					var val int
					if _, err := fmt.Sscanf(decStr, "%d", &val); err == nil && val > 0 && val < 0x110000 {
						b.WriteRune(rune(val))
						i += 2 + end + 1
						continue
					}
				}
			}
			// Named entities.
			for _, ent := range namedEntities {
				if strings.HasPrefix(s[i:], ent.entity) {
					b.WriteString(ent.replacement)
					i += len(ent.entity)
					goto next
				}
			}
			b.WriteByte(s[i])
			i++
			continue
		}
		if s[i] == '%' && i+2 < len(s) {
			// URL percent-encoding: %HH
			var val int
			if _, err := fmt.Sscanf(s[i+1:i+3], "%02x", &val); err == nil {
				b.WriteByte(byte(val))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
		i++
		continue
	next:
	}
	return b.String()
}

type htmlEntity struct {
	entity      string
	replacement string
}

var namedEntities = []htmlEntity{
	{"&lt;", "<"},
	{"&gt;", ">"},
	{"&amp;", "&"},
	{"&quot;", "\""},
	{"&apos;", "'"},
}
