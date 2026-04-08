package tui

import (
	"strings"
	"testing"
)

func TestRenderMarkdown_Headers(t *testing.T) {
	input := "# Hello World\n## Sub Section"
	result := RenderMarkdown(input, 80)
	if !strings.Contains(result, "HELLO WORLD") {
		t.Fatalf("expected uppercase header, got: %s", result)
	}
	if !strings.Contains(result, "SUB SECTION") {
		t.Fatalf("expected uppercase sub-header, got: %s", result)
	}
}

func TestRenderMarkdown_Bold(t *testing.T) {
	input := "This is **bold** text"
	result := RenderMarkdown(input, 80)
	if !strings.Contains(result, "This is bold text") {
		t.Fatalf("expected bold stripped, got: %s", result)
	}
}

func TestRenderMarkdown_Italic(t *testing.T) {
	input := "This is *italic* text"
	result := RenderMarkdown(input, 80)
	if !strings.Contains(result, "This is italic text") {
		t.Fatalf("expected italic stripped, got: %s", result)
	}
}

func TestRenderMarkdown_InlineCode(t *testing.T) {
	input := "Use `fmt.Println` here"
	result := RenderMarkdown(input, 80)
	if !strings.Contains(result, "Use fmt.Println here") {
		t.Fatalf("expected inline code stripped, got: %s", result)
	}
}

func TestRenderMarkdown_Links(t *testing.T) {
	input := "Visit [Google](https://google.com) now"
	result := RenderMarkdown(input, 80)
	if !strings.Contains(result, "Google [https://google.com]") {
		t.Fatalf("expected link format, got: %s", result)
	}
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	input := "```go\nfmt.Println(\"hello\")\n```"
	result := RenderMarkdown(input, 80)
	if !strings.Contains(result, "    fmt.Println") {
		t.Fatalf("expected indented code block, got: %s", result)
	}
}

func TestRenderMarkdown_WordWrap(t *testing.T) {
	input := "This is a very long line that should be wrapped at word boundaries for terminal display"
	result := RenderMarkdown(input, 30)
	for _, line := range strings.Split(result, "\n") {
		if len(line) > 35 { // some tolerance for single long words
			t.Fatalf("line too long: %q (%d chars)", line, len(line))
		}
	}
}

func TestRenderMarkdown_DefaultWidth(t *testing.T) {
	result := RenderMarkdown("hello", 0)
	if result != "hello" {
		t.Fatalf("unexpected: %s", result)
	}
}
