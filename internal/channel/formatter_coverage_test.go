package channel

import (
	"strings"
	"testing"
)

func TestFormatFor_AllPlatforms(t *testing.T) {
	platforms := []string{"telegram", "discord", "whatsapp", "signal", "email", "web", "unknown"}
	for _, p := range platforms {
		f := FormatFor(p)
		if f == nil {
			t.Errorf("FormatFor(%q) returned nil", p)
		}
	}
}

func TestTelegramFormatter_MarkdownV2(t *testing.T) {
	f := FormatFor("telegram")
	result := f.Format("Hello **bold** and `code`")
	if !strings.Contains(result, "bold") {
		t.Error("should preserve bold text")
	}
}

func TestTelegramFormatter_CodeBlockCoverage(t *testing.T) {
	f := FormatFor("telegram")
	result := f.Format("```python\nprint('hello')\n```")
	if !strings.Contains(result, "print") {
		t.Error("should preserve code blocks")
	}
}

func TestDiscordFormatter_HandlesLong(t *testing.T) {
	f := FormatFor("discord")
	long := strings.Repeat("a", 3000)
	result := f.Format(long)
	// Discord formatter should handle long content (may truncate or pass through).
	if len(result) == 0 {
		t.Error("should return non-empty for long input")
	}
}

func TestWhatsAppFormatter_Format(t *testing.T) {
	f := FormatFor("whatsapp")
	result := f.Format("**bold** and _italic_")
	if result == "" {
		t.Error("should return non-empty")
	}
}

func TestSignalFormatter_Format(t *testing.T) {
	f := FormatFor("signal")
	result := f.Format("Hello world")
	if result == "" {
		t.Error("should return non-empty")
	}
}

func TestEmailFormatter_Format(t *testing.T) {
	f := FormatFor("email")
	result := f.Format("# Header\n\nParagraph text")
	if !strings.Contains(result, "Header") {
		t.Error("should preserve header")
	}
}

func TestWebFormatter_Passthrough(t *testing.T) {
	f := FormatFor("web")
	input := "Hello **world**"
	result := f.Format(input)
	if result != input {
		t.Errorf("web formatter should passthrough, got %q", result)
	}
}
