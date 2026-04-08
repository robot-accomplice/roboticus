package agent

import (
	"regexp"
	"strings"
	"unicode"
)

// FilterToolOutput applies the Rust-parity tool output filter chain:
//  1. ANSI escape stripping (CSI + OSC sequences)
//  2. Progress line removal (>60% progress chars with %)
//  3. Consecutive duplicate line dedup
//  4. Whitespace normalization
//
// This prevents noisy tool output from consuming context window budget
// and confusing subsequent inference turns.
func FilterToolOutput(raw string) string {
	s := stripANSI(raw)
	s = filterProgressLines(s)
	s = dedupConsecutiveLines(s)
	s = normalizeWhitespace(s)
	return s
}

// ansiPattern matches CSI sequences: ESC[ ... final byte
// and OSC sequences: ESC] ... ST (BEL or ESC\)
var ansiCSI = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
var ansiOSC = regexp.MustCompile(`\x1b\][^\x07\x1b]*[\x07]|\x1b\][^\x07\x1b]*\x1b\\`)

// stripANSI removes all ANSI escape sequences from the string.
func stripANSI(s string) string {
	s = ansiCSI.ReplaceAllString(s, "")
	s = ansiOSC.ReplaceAllString(s, "")
	// Also strip bare ESC followed by single char (SS2, SS3, etc.)
	result := strings.Builder{}
	skip := false
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			skip = true
			continue
		}
		if skip {
			skip = false
			// Skip the character after ESC if it's a control char.
			if s[i] >= 0x40 && s[i] <= 0x7e {
				continue
			}
		}
		result.WriteByte(s[i])
	}
	return result.String()
}

// progressChars are characters commonly found in progress bars.
var progressChars = map[rune]bool{
	'█': true, '▓': true, '▒': true, '░': true,
	'=': true, '-': true, '#': true, '·': true,
	'▏': true, '▎': true, '▍': true, '▌': true,
	'▋': true, '▊': true, '▉': true,
}

// filterProgressLines removes lines that appear to be progress bar output.
// A line is considered a progress line if >60% of its characters are
// progress bar characters AND it contains a '%' sign.
func filterProgressLines(s string) string {
	lines := strings.Split(s, "\n")
	var filtered []string
	for _, line := range lines {
		if isProgressLine(line) {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func isProgressLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 5 || !strings.Contains(trimmed, "%") {
		return false
	}
	progressCount := 0
	total := 0
	for _, r := range trimmed {
		if !unicode.IsSpace(r) {
			total++
			if progressChars[r] {
				progressCount++
			}
		}
	}
	if total == 0 {
		return false
	}
	return float64(progressCount)/float64(total) > 0.6
}

// dedupConsecutiveLines removes consecutive identical lines, keeping
// the first occurrence. This handles common patterns like repeated
// status updates or compilation output.
func dedupConsecutiveLines(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= 1 {
		return s
	}
	var result []string
	result = append(result, lines[0])
	for i := 1; i < len(lines); i++ {
		if lines[i] != lines[i-1] {
			result = append(result, lines[i])
		}
	}
	return strings.Join(result, "\n")
}

// normalizeWhitespace collapses excessive whitespace: more than 2
// consecutive blank lines become 2, and trailing whitespace is trimmed.
func normalizeWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	blankCount := 0
	for _, line := range lines {
		trimmed := strings.TrimRightFunc(line, unicode.IsSpace)
		if trimmed == "" {
			blankCount++
			if blankCount <= 2 {
				result = append(result, "")
			}
		} else {
			blankCount = 0
			result = append(result, trimmed)
		}
	}
	return strings.TrimRight(strings.Join(result, "\n"), "\n")
}
