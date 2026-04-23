package tools

import (
	"regexp"
	"strings"
	"unicode"
)

// FilterToolOutput applies the canonical tool-output filter chain:
//  1. ANSI escape stripping
//  2. progress-line removal
//  3. consecutive duplicate-line dedup
//  4. whitespace normalization
func FilterToolOutput(raw string) string {
	s := stripANSI(raw)
	s = filterProgressLines(s)
	s = dedupConsecutiveLines(s)
	s = normalizeWhitespace(s)
	return s
}

var ansiCSI = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
var ansiOSC = regexp.MustCompile(`\x1b\][^\x07\x1b]*[\x07]|\x1b\][^\x07\x1b]*\x1b\\`)

func stripANSI(s string) string {
	s = ansiCSI.ReplaceAllString(s, "")
	s = ansiOSC.ReplaceAllString(s, "")
	result := strings.Builder{}
	skip := false
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			skip = true
			continue
		}
		if skip {
			skip = false
			if s[i] >= 0x40 && s[i] <= 0x7e {
				continue
			}
		}
		result.WriteByte(s[i])
	}
	return result.String()
}

var progressChars = map[rune]bool{
	'█': true, '▓': true, '▒': true, '░': true,
	'=': true, '-': true, '#': true, '·': true,
	'▏': true, '▎': true, '▍': true, '▌': true,
	'▋': true, '▊': true, '▉': true,
}

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
