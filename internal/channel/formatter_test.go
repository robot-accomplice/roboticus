package channel

import (
	"strings"
	"testing"
)

func TestPreprocess(t *testing.T) {
	input := "[Delegated to agent-1] task done\n[Tool call] search\n---orchestration: begin\n\nSome text [3] here\n\n\n\nMore text"
	result := preprocess(input)

	if strings.Contains(result, "[Delegated") {
		t.Error("should strip delegation markers")
	}
	if strings.Contains(result, "[Tool call]") {
		t.Error("should strip tool call markers")
	}
	if strings.Contains(result, "---orchestration") {
		t.Error("should strip orchestration markers")
	}
	if strings.Contains(result, "[3]") {
		t.Error("should strip bracket citations")
	}
	if strings.Contains(result, "\n\n\n") {
		t.Error("should collapse multiple blank lines")
	}
}

func TestFormatFor_ReturnsCorrectFormatter(t *testing.T) {
	cases := map[string]string{
		"telegram": "telegram",
		"discord":  "discord",
		"whatsapp": "whatsapp",
		"signal":   "signal",
		"email":    "email",
		"unknown":  "web",
		"Telegram": "telegram",
	}
	for input, want := range cases {
		f := FormatFor(input)
		if f.Platform() != want {
			t.Errorf("FormatFor(%q).Platform() = %q, want %q", input, f.Platform(), want)
		}
	}
}

func TestTelegramFormatter_Headers(t *testing.T) {
	f := &TelegramFormatter{}
	result := f.Format("# Hello World")
	if !strings.Contains(result, "*") {
		t.Error("headers should be converted to bold")
	}
	if strings.Contains(result, "# ") {
		t.Error("header prefix should be removed")
	}
}

func TestTelegramFormatter_CodeBlock(t *testing.T) {
	f := &TelegramFormatter{}
	input := "Text before\n```go\nfunc main() {\n}\n```\nText after"
	result := f.Format(input)
	if !strings.Contains(result, "func main()") {
		t.Error("code block content should be preserved")
	}
}

func TestDiscordFormatter_PassThrough(t *testing.T) {
	f := &DiscordFormatter{}
	input := "**bold** _italic_ `code`"
	result := f.Format(input)
	if result != input {
		t.Errorf("discord should pass through markdown, got %q", result)
	}
}

func TestWhatsAppFormatter_Bold(t *testing.T) {
	f := &WhatsAppFormatter{}
	result := f.Format("**important** text")
	if strings.Contains(result, "**") {
		t.Error("** should be converted to *")
	}
	if !strings.Contains(result, "*important*") {
		t.Error("bold should use single asterisks")
	}
}

func TestWhatsAppFormatter_Links(t *testing.T) {
	f := &WhatsAppFormatter{}
	result := f.Format("Check [this](https://example.com) out")
	if strings.Contains(result, "[this]") {
		t.Error("link syntax should be removed")
	}
	if !strings.Contains(result, "https://example.com") {
		t.Error("URL should be preserved")
	}
}

func TestSignalFormatter_StripsMarkdown(t *testing.T) {
	f := &SignalFormatter{}
	result := f.Format("**bold** and `code`")
	if strings.Contains(result, "**") || strings.Contains(result, "`") {
		t.Error("signal should strip all markdown")
	}
	// Rust parity: Signal prefixes with 🤖.
	if !strings.HasPrefix(result, "🤖") {
		t.Errorf("signal should prefix with robot emoji, got %q", result)
	}
}

func TestSignalFormatter_CodeBlockIndent(t *testing.T) {
	f := &SignalFormatter{}
	result := f.Format("text\n```\ncode line\n```\nmore text")
	if !strings.Contains(result, "  code line") {
		t.Error("code blocks should be indented with 2 spaces")
	}
}

func TestSignalFormatter_EmptyReturnsEmpty(t *testing.T) {
	f := &SignalFormatter{}
	result := f.Format("")
	if result != "" {
		t.Errorf("empty input should return empty, got %q", result)
	}
}
