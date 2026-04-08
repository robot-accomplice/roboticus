package channel

import (
	"strings"
	"testing"
)

// TestFormatter_AllPlatforms_ProduceOutput verifies every formatter produces
// non-empty output for a representative input.
func TestFormatter_AllPlatforms_ProduceOutput(t *testing.T) {
	input := "# Title\n\nHello **world** and `code`.\n\n```python\nprint('hi')\n```\n\n- item 1\n- item 2\n\n[link](https://example.com)"

	platforms := []string{"telegram", "discord", "whatsapp", "signal", "email", "web", "voice", "matrix"}
	for _, p := range platforms {
		f := FormatFor(p)
		result := f.Format(input)
		if result == "" {
			t.Errorf("%s formatter returned empty", p)
		}
		if f.Platform() != p {
			t.Errorf("%s formatter Platform() = %q", p, f.Platform())
		}
	}
}

// TestTelegramFormatter_EscapesSpecialChars verifies MarkdownV2 escaping.
func TestTelegramFormatter_EscapesSpecialChars(t *testing.T) {
	f := FormatFor("telegram")
	result := f.Format("Hello (world) [test]")
	// Telegram MarkdownV2 requires escaping ( ) [ ]
	if strings.Contains(result, "(world)") {
		t.Error("should escape parentheses for MarkdownV2")
	}
}

// TestTelegramFormatter_PreservesCodeBlocks ensures code blocks aren't escaped.
func TestTelegramFormatter_PreservesCodeBlocks(t *testing.T) {
	f := FormatFor("telegram")
	result := f.Format("text\n```\ncode (special)\n```\nmore")
	// Inside code blocks, special chars should NOT be escaped.
	if !strings.Contains(result, "code (special)") {
		t.Error("should preserve code block contents unescaped")
	}
}

// TestSignalFormatter_StripsAllMarkdown verifies plain text output.
func TestSignalFormatter_StripsAllMarkdown(t *testing.T) {
	f := FormatFor("signal")
	result := f.Format("**bold** and __underline__ and ~~strike~~")
	if strings.Contains(result, "**") || strings.Contains(result, "__") || strings.Contains(result, "~~") {
		t.Error("should strip all markdown formatting for Signal")
	}
	if !strings.Contains(result, "bold") {
		t.Error("should preserve text content")
	}
}

// TestSignalFormatter_ExtractsLinkURLs verifies link URL extraction.
func TestSignalFormatter_ExtractsLinkURLs(t *testing.T) {
	f := FormatFor("signal")
	result := f.Format("Check [this link](https://example.com)")
	if !strings.Contains(result, "https://example.com") {
		t.Error("should extract URL from markdown link")
	}
}

// TestWhatsAppFormatter_BoldConversion verifies ** → * conversion.
func TestWhatsAppFormatter_BoldConversion(t *testing.T) {
	f := FormatFor("whatsapp")
	result := f.Format("This is **bold** text")
	if strings.Contains(result, "**") {
		t.Error("should convert ** to * for WhatsApp")
	}
	if !strings.Contains(result, "*bold*") {
		t.Error("should wrap bold text in single asterisks")
	}
}

// TestVoiceFormatter_StripsEverything verifies TTS-clean output.
func TestVoiceFormatter_StripsEverything(t *testing.T) {
	f := FormatFor("voice")
	result := f.Format("# Title\n\n**bold** text.\n\n```python\nprint('hi')\n```\n\n[link](https://example.com)")
	if strings.Contains(result, "#") {
		t.Error("should strip header markers")
	}
	if strings.Contains(result, "**") {
		t.Error("should strip bold markers")
	}
	if strings.Contains(result, "```") {
		t.Error("should strip code fences")
	}
	if strings.Contains(result, "print") {
		t.Error("should strip code block contents")
	}
	if strings.Contains(result, "https://") {
		t.Error("should strip URLs, keeping only link text")
	}
	if !strings.Contains(result, "link") {
		t.Error("should keep link text")
	}
	if !strings.Contains(result, "Title") {
		t.Error("should keep header text")
	}
}

// TestMatrixFormatter_HTMLConversion verifies HTML output for Matrix.
func TestMatrixFormatter_HTMLConversion(t *testing.T) {
	f := FormatFor("matrix")
	result := f.Format("# Title\n\nHello.\n\n```python\ncode here\n```")
	if !strings.Contains(result, "<h1>") {
		t.Error("should convert # to <h1>")
	}
	if !strings.Contains(result, "<pre><code") {
		t.Error("should convert code blocks to <pre><code>")
	}
	if !strings.Contains(result, "language-python") {
		t.Error("should include language class on code block")
	}
}

// TestEmailFormatter_PreservesMarkdown verifies pass-through for email.
func TestEmailFormatter_PreservesMarkdown(t *testing.T) {
	f := FormatFor("email")
	result := f.Format("# Title\n\n**bold** and `code`")
	if !strings.Contains(result, "# Title") {
		t.Error("should preserve markdown headers for email")
	}
	if !strings.Contains(result, "**bold**") {
		t.Error("should preserve bold for email")
	}
}

// TestFormatter_InternalMetadataStripped verifies pre-processing removes internal markers.
func TestFormatter_InternalMetadataStripped(t *testing.T) {
	input := "[Delegated to subagent: risk_management]\nHello world\n[Tool call: bash]\nGoodbye"
	for _, p := range []string{"telegram", "discord", "signal", "whatsapp"} {
		f := FormatFor(p)
		result := f.Format(input)
		if strings.Contains(result, "Delegated to") {
			t.Errorf("%s: should strip [Delegated to ...] metadata", p)
		}
		if strings.Contains(result, "Tool call") {
			t.Errorf("%s: should strip [Tool call ...] metadata", p)
		}
		if !strings.Contains(result, "Hello world") {
			t.Errorf("%s: should preserve normal content", p)
		}
	}
}

// TestFormatter_BracketCitationsStripped verifies [1] [23] removal.
func TestFormatter_BracketCitationsStripped(t *testing.T) {
	input := "This is a fact[1] and another[23]."
	f := FormatFor("discord")
	result := f.Format(input)
	if strings.Contains(result, "[1]") || strings.Contains(result, "[23]") {
		t.Error("should strip bracket citations")
	}
}
