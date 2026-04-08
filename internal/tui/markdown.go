package tui

import (
	"regexp"
	"strings"
)

var (
	reHeader     = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)
	reBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic     = regexp.MustCompile(`\*(.+?)\*`)
	reInlineCode = regexp.MustCompile("`([^`]+)`")
	reLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reCodeBlock  = regexp.MustCompile("(?ms)^```[a-zA-Z]*\n(.*?)^```")
	reHR         = regexp.MustCompile(`(?m)^---+\s*$`)
)

// RenderMarkdown converts Markdown to plain terminal-friendly text.
// It strips formatting: bold becomes plain text, headers become UPPERCASE,
// code blocks become indented, and links show as "text [url]".
func RenderMarkdown(md string, width int) string {
	if width <= 0 {
		width = 80
	}

	// Process code blocks first (before inline rules eat backticks).
	md = reCodeBlock.ReplaceAllStringFunc(md, func(block string) string {
		inner := reCodeBlock.FindStringSubmatch(block)
		if len(inner) < 2 {
			return block
		}
		var lines []string
		for _, line := range strings.Split(inner[1], "\n") {
			lines = append(lines, "    "+line)
		}
		return strings.Join(lines, "\n")
	})

	// Headers -> UPPERCASE.
	md = reHeader.ReplaceAllStringFunc(md, func(h string) string {
		match := reHeader.FindStringSubmatch(h)
		if len(match) < 3 {
			return h
		}
		return strings.ToUpper(match[2])
	})

	// Bold -> plain text.
	md = reBold.ReplaceAllString(md, "$1")

	// Italic -> plain text.
	md = reItalic.ReplaceAllString(md, "$1")

	// Inline code -> plain text.
	md = reInlineCode.ReplaceAllString(md, "$1")

	// Links -> text [url].
	md = reLink.ReplaceAllString(md, "$1 [$2]")

	// Horizontal rules -> dashes.
	md = reHR.ReplaceAllStringFunc(md, func(_ string) string {
		if width > 3 {
			return strings.Repeat("-", width)
		}
		return "---"
	})

	// Wrap lines to width.
	var result []string
	for _, line := range strings.Split(md, "\n") {
		if len(line) <= width {
			result = append(result, line)
			continue
		}
		result = append(result, wrapLine(line, width)...)
	}

	return strings.Join(result, "\n")
}

// wrapLine breaks a single line into multiple lines at word boundaries.
func wrapLine(line string, width int) []string {
	if width <= 0 {
		return []string{line}
	}

	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{""}
	}

	// Preserve leading whitespace.
	indent := ""
	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			indent += string(ch)
		} else {
			break
		}
	}

	var lines []string
	current := indent + words[0]
	for _, word := range words[1:] {
		if len(current)+1+len(word) > width {
			lines = append(lines, current)
			current = indent + word
		} else {
			current += " " + word
		}
	}
	lines = append(lines, current)
	return lines
}
