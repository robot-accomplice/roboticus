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

// TestWhatsAppFormatter_InlineCode verifies `code` → ```code``` (WhatsApp monospace).
func TestWhatsAppFormatter_InlineCode(t *testing.T) {
	f := FormatFor("whatsapp")
	result := f.Format("Use the `print()` function")
	if !strings.Contains(result, "```print()```") {
		t.Errorf("inline code should become triple-backtick monospace, got %q", result)
	}
}

// TestWhatsAppFormatter_HeaderH3 verifies ### header conversion.
func TestWhatsAppFormatter_HeaderH3(t *testing.T) {
	f := FormatFor("whatsapp")
	result := f.Format("### Section Title")
	if !strings.Contains(result, "*Section Title*") {
		t.Errorf("h3 not converted to bold: got %q", result)
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

// --- Telegram inline formatting parity tests (Rust formatter.rs) ---

// TestTelegramFormatter_BoldConversion verifies **bold** → *bold* with escaped content.
func TestTelegramFormatter_BoldConversion(t *testing.T) {
	f := FormatFor("telegram")
	result := f.Format("This is **important** text")
	// **bold** should become *bold* (Telegram MarkdownV2 bold).
	if !strings.Contains(result, "*important*") {
		t.Errorf("bold not converted: got %q", result)
	}
	// Must NOT contain double asterisks.
	if strings.Contains(result, "**") {
		t.Errorf("double asterisks should be converted to single: got %q", result)
	}
}

// TestTelegramFormatter_ItalicConversion verifies *italic* → _italic_.
func TestTelegramFormatter_ItalicConversion(t *testing.T) {
	f := FormatFor("telegram")
	result := f.Format("This is *subtle* emphasis")
	// *italic* should become _italic_ (Telegram MarkdownV2 italic).
	if !strings.Contains(result, "_subtle_") {
		t.Errorf("italic not converted: got %q", result)
	}
}

// TestTelegramFormatter_Strikethrough verifies ~~strike~~ → ~strike~.
func TestTelegramFormatter_Strikethrough(t *testing.T) {
	f := FormatFor("telegram")
	result := f.Format("This is ~~removed~~ text")
	if !strings.Contains(result, "~removed~") {
		t.Errorf("strikethrough not converted: got %q", result)
	}
	if strings.Contains(result, "~~") {
		t.Errorf("double tildes should be converted to single: got %q", result)
	}
}

// TestTelegramFormatter_InlineCode verifies `code` passes through.
func TestTelegramFormatter_InlineCode(t *testing.T) {
	f := FormatFor("telegram")
	result := f.Format("Use the `fmt.Println()` function")
	// Inline code should be preserved with backticks, content NOT escaped.
	if !strings.Contains(result, "`fmt.Println()`") {
		t.Errorf("inline code not preserved: got %q", result)
	}
}

// TestTelegramFormatter_Blockquote verifies > text → >text.
func TestTelegramFormatter_Blockquote(t *testing.T) {
	f := FormatFor("telegram")
	result := f.Format("> This is a quote")
	// Blockquote should start with > (no space) in MarkdownV2.
	if !strings.HasPrefix(result, ">") {
		t.Errorf("blockquote prefix missing: got %q", result)
	}
	if strings.Contains(result, "\\>") {
		t.Errorf("blockquote > should not be escaped: got %q", result)
	}
}

// TestTelegramFormatter_Link verifies [text](url) → [escaped text](url).
func TestTelegramFormatter_Link(t *testing.T) {
	f := FormatFor("telegram")
	result := f.Format("Check [this link](https://example.com) out")
	if !strings.Contains(result, "[this link](https://example.com)") {
		t.Errorf("link not properly formatted: got %q", result)
	}
}

// TestTelegramFormatter_MixedFormatting verifies a realistic LLM response.
func TestTelegramFormatter_MixedFormatting(t *testing.T) {
	f := FormatFor("telegram")
	input := "**The Interpretation:**\n\n*   **The Struggle:** It strips away the layers.\n*   **The Need:** It boils down to survival.\n\n> \"The desert does not care.\"\n\nHow does that *feel*?"
	result := f.Format(input)

	// Bold headers should render as *text* not \*\*text\*\*.
	if strings.Contains(result, "\\*\\*") {
		t.Errorf("bold should be converted, not escaped: got %q", result)
	}

	// Blockquote should be preserved.
	if !strings.Contains(result, ">") {
		t.Errorf("blockquote lost: got %q", result)
	}

	// Italic should convert to underscores.
	if !strings.Contains(result, "_feel_") {
		t.Errorf("italic not converted: got %q", result)
	}
}

// TestTelegramFormatter_SpecialCharsInPlainText verifies escaping of non-formatting chars.
func TestTelegramFormatter_SpecialCharsInPlainText(t *testing.T) {
	f := FormatFor("telegram")
	result := f.Format("Hello (world). Test!")
	// Parens, period, and exclamation must be escaped in plain text.
	if !strings.Contains(result, "\\(world\\)") {
		t.Errorf("parentheses not escaped: got %q", result)
	}
	if !strings.Contains(result, "\\.") {
		t.Errorf("period not escaped: got %q", result)
	}
	if !strings.Contains(result, "\\!") {
		t.Errorf("exclamation not escaped: got %q", result)
	}
}

// TestTelegramFormatter_HeaderH3 verifies ### header handling.
func TestTelegramFormatter_HeaderH3(t *testing.T) {
	f := FormatFor("telegram")
	result := f.Format("### Subsection Title")
	if !strings.Contains(result, "*Subsection Title*") {
		t.Errorf("h3 not converted to bold: got %q", result)
	}
}
