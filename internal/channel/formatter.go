package channel

import (
	"regexp"
	"strings"
)

var mdLinkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

// Formatter converts LLM Markdown output into platform-native syntax.
type Formatter interface {
	Platform() string
	Format(content string) string
}

// FormatFor returns a formatter for the given platform.
// Unknown platforms fall back to WebFormatter (preserves Markdown).
func FormatFor(platform string) Formatter {
	switch strings.ToLower(platform) {
	case "telegram":
		return &TelegramFormatter{}
	case "discord":
		return &DiscordFormatter{}
	case "whatsapp":
		return &WhatsAppFormatter{}
	case "signal":
		return &SignalFormatter{}
	case "email":
		return &EmailFormatter{}
	case "voice":
		return &VoiceFormatter{}
	case "matrix":
		return &MatrixFormatter{}
	default:
		return &WebFormatter{}
	}
}

// --- Pre-processing (all platforms) ---

var (
	internalMetadataRe = regexp.MustCompile(`(?m)^\[(Delegated to|Delegation from|Tool call|Tool result|Internal).*\].*$`)
	orchestrationRe    = regexp.MustCompile(`(?m)^---orchestration.*$`)
	bracketCitationRe  = regexp.MustCompile(`\[\d+\]`)
	multiBlankRe       = regexp.MustCompile(`\n{3,}`)
)

func preprocess(content string) string {
	content = internalMetadataRe.ReplaceAllString(content, "")
	content = orchestrationRe.ReplaceAllString(content, "")
	content = bracketCitationRe.ReplaceAllString(content, "")
	content = multiBlankRe.ReplaceAllString(content, "\n\n")
	return strings.TrimSpace(content)
}

// --- Telegram Formatter ---

// TelegramFormatter converts Markdown to Telegram MarkdownV2.
// It parses inline formatting (bold, italic, strikethrough, code, links,
// blockquotes) character-by-character, converting Markdown constructs to
// their MarkdownV2 equivalents while escaping all other special characters.
type TelegramFormatter struct{}

func (f *TelegramFormatter) Platform() string { return "telegram" }

// telegramSpecialChars contains all characters that must be escaped in
// Telegram MarkdownV2 text segments (outside of formatting delimiters).
const telegramSpecialChars = `_*[]()~` + "`>#+-.=|{}!"

func telegramEscapeText(text string) string {
	var b strings.Builder
	b.Grow(len(text) * 2)
	for _, ch := range text {
		if strings.ContainsRune(telegramSpecialChars, ch) {
			b.WriteByte('\\')
		}
		b.WriteRune(ch)
	}
	return b.String()
}

func (f *TelegramFormatter) Format(content string) string {
	content = preprocess(content)

	var out []string
	inFence := false

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		// Code fence boundaries.
		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				out = append(out, "```")
				inFence = false
			} else {
				// Preserve language hint.
				out = append(out, trimmed)
				inFence = true
			}
			continue
		}
		if inFence {
			// Inside code block — no escaping, no conversion.
			out = append(out, line)
			continue
		}

		out = append(out, telegramConvertLine(trimmed))
	}

	// Close unclosed fence.
	if inFence {
		out = append(out, "```")
	}

	return strings.TrimSpace(strings.Join(out, "\n"))
}

// telegramConvertLine converts a single line of Markdown to Telegram MarkdownV2.
// Handles: **bold** → *bold*, *italic* → _italic_, ~~strike~~ → ~strike~,
// `code` → `code`, [text](url) → [text](url), > blockquotes, # headers → bold.
func telegramConvertLine(line string) string {
	// Headers → bold.
	if rest, ok := strings.CutPrefix(line, "### "); ok {
		return "*" + telegramEscapeText(strings.TrimSpace(rest)) + "*"
	}
	if rest, ok := strings.CutPrefix(line, "## "); ok {
		return "*" + telegramEscapeText(strings.TrimSpace(rest)) + "*"
	}
	if rest, ok := strings.CutPrefix(line, "# "); ok {
		return "*" + telegramEscapeText(strings.TrimSpace(rest)) + "*"
	}

	// Blockquote: > text → >text (Telegram MarkdownV2 blockquote).
	if rest, ok := strings.CutPrefix(line, "> "); ok {
		return ">" + telegramConvertInline(rest)
	}
	if line == ">" {
		return ">"
	}

	return telegramConvertInline(line)
}

// telegramConvertInline parses inline Markdown formatting and converts to
// Telegram MarkdownV2, escaping plain text segments. This is a character-
// level parser that handles nested/overlapping formatting correctly.
func telegramConvertInline(text string) string {
	chars := []rune(text)
	n := len(chars)
	var b strings.Builder
	b.Grow(n * 2)
	i := 0

	for i < n {
		// Inline code: `code`
		if chars[i] == '`' && i+1 < n {
			if end, ok := findClosing(chars, i+1, '`'); ok {
				b.WriteByte('`')
				b.WriteString(string(chars[i+1 : end])) // no escaping inside code
				b.WriteByte('`')
				i = end + 1
				continue
			}
		}

		// Bold: **text** → *text*
		if i+1 < n && chars[i] == '*' && chars[i+1] == '*' {
			if end, ok := findDoubleClosing(chars, i+2, '*'); ok {
				inner := string(chars[i+2 : end])
				b.WriteByte('*')
				b.WriteString(telegramEscapeText(inner))
				b.WriteByte('*')
				i = end + 2
				continue
			}
		}

		// Strikethrough: ~~text~~ → ~text~
		if i+1 < n && chars[i] == '~' && chars[i+1] == '~' {
			if end, ok := findDoubleClosing(chars, i+2, '~'); ok {
				inner := string(chars[i+2 : end])
				b.WriteByte('~')
				b.WriteString(telegramEscapeText(inner))
				b.WriteByte('~')
				i = end + 2
				continue
			}
		}

		// Single-tilde strikethrough: ~text~ (some LLMs emit this).
		if chars[i] == '~' && (i == 0 || chars[i-1] != '~') && i+1 < n && chars[i+1] != '~' {
			if end, ok := findClosingNotDoubled(chars, i+1, '~'); ok {
				inner := string(chars[i+1 : end])
				b.WriteByte('~')
				b.WriteString(telegramEscapeText(inner))
				b.WriteByte('~')
				i = end + 1
				continue
			}
		}

		// Italic: *text* (single asterisk) → _text_
		if chars[i] == '*' && (i == 0 || chars[i-1] != '*') && i+1 < n && chars[i+1] != '*' {
			if end, ok := findClosingNotDoubled(chars, i+1, '*'); ok {
				inner := string(chars[i+1 : end])
				b.WriteByte('_')
				b.WriteString(telegramEscapeText(inner))
				b.WriteByte('_')
				i = end + 1
				continue
			}
		}

		// Italic: __text__ → _text_
		if i+1 < n && chars[i] == '_' && chars[i+1] == '_' {
			if end, ok := findDoubleClosing(chars, i+2, '_'); ok {
				inner := string(chars[i+2 : end])
				b.WriteByte('_')
				b.WriteString(telegramEscapeText(inner))
				b.WriteByte('_')
				i = end + 2
				continue
			}
		}

		// Italic: _text_ (single underscores).
		if chars[i] == '_' && (i == 0 || chars[i-1] != '_') && i+1 < n && chars[i+1] != '_' {
			if end, ok := findClosingNotDoubled(chars, i+1, '_'); ok {
				inner := string(chars[i+1 : end])
				b.WriteByte('_')
				b.WriteString(telegramEscapeText(inner))
				b.WriteByte('_')
				i = end + 1
				continue
			}
		}

		// Markdown link: [text](url) → [text](url)
		if chars[i] == '[' {
			if linkText, url, endPos, ok := parseMarkdownLink(chars, i); ok {
				b.WriteByte('[')
				b.WriteString(telegramEscapeText(linkText))
				b.WriteString("](")
				b.WriteString(url) // URLs don't get escaped
				b.WriteByte(')')
				i = endPos
				continue
			}
		}

		// Regular character — escape if special.
		if strings.ContainsRune(telegramSpecialChars, chars[i]) {
			b.WriteByte('\\')
		}
		b.WriteRune(chars[i])
		i++
	}

	return b.String()
}

// findClosing finds the next occurrence of delim starting at start,
// skipping backslash-escaped characters.
func findClosing(chars []rune, start int, delim rune) (int, bool) {
	i := start
	for i < len(chars) {
		if chars[i] == '\\' {
			i += 2
			continue
		}
		if chars[i] == delim {
			return i, true
		}
		i++
	}
	return 0, false
}

// findDoubleClosing finds the next occurrence of two consecutive delim
// characters starting at start.
func findDoubleClosing(chars []rune, start int, delim rune) (int, bool) {
	i := start
	for i+1 < len(chars) {
		if chars[i] == '\\' {
			i += 2
			continue
		}
		if chars[i] == delim && chars[i+1] == delim {
			return i, true
		}
		i++
	}
	return 0, false
}

// findClosingNotDoubled finds the next single occurrence of delim that is
// NOT immediately followed by another delim (to distinguish * from **).
func findClosingNotDoubled(chars []rune, start int, delim rune) (int, bool) {
	i := start
	for i < len(chars) {
		if chars[i] == '\\' {
			i += 2
			continue
		}
		if chars[i] == delim {
			if i+1 < len(chars) && chars[i+1] == delim {
				i += 2 // skip doubled
				continue
			}
			return i, true
		}
		i++
	}
	return 0, false
}

// parseMarkdownLink parses [text](url) starting at chars[start].
// Returns (linkText, url, endPosition, ok).
func parseMarkdownLink(chars []rune, start int) (string, string, int, bool) {
	if start >= len(chars) || chars[start] != '[' {
		return "", "", 0, false
	}
	i := start + 1
	depth := 1
	for i < len(chars) && depth > 0 {
		if chars[i] == '[' {
			depth++
		}
		if chars[i] == ']' {
			depth--
		}
		if depth > 0 {
			i++
		}
	}
	if depth != 0 || i >= len(chars) {
		return "", "", 0, false
	}
	linkText := string(chars[start+1 : i])
	i++ // skip ]
	if i >= len(chars) || chars[i] != '(' {
		return "", "", 0, false
	}
	i++ // skip (
	urlStart := i
	parenDepth := 1
	for i < len(chars) && parenDepth > 0 {
		if chars[i] == '(' {
			parenDepth++
		}
		if chars[i] == ')' {
			parenDepth--
		}
		if parenDepth > 0 {
			i++
		}
	}
	if parenDepth != 0 {
		return "", "", 0, false
	}
	url := string(chars[urlStart:i])
	return linkText, url, i + 1, true
}

// --- Discord Formatter ---

// DiscordFormatter passes through Markdown (Discord supports it natively).
type DiscordFormatter struct{}

func (f *DiscordFormatter) Platform() string { return "discord" }
func (f *DiscordFormatter) Format(content string) string {
	return preprocess(content)
}

// --- WhatsApp Formatter ---

// WhatsAppFormatter converts Markdown to WhatsApp Cloud API syntax.
// WhatsApp supports: *bold*, _italic_, ~strikethrough~, ```monospace```.
// Markdown links become bare URLs.
type WhatsAppFormatter struct{}

func (f *WhatsAppFormatter) Platform() string { return "whatsapp" }

func (f *WhatsAppFormatter) Format(content string) string {
	content = preprocess(content)

	var out []string
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				out = append(out, "```")
				inFence = false
			} else {
				out = append(out, "```") // strip language hint
				inFence = true
			}
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		out = append(out, whatsappConvertLine(line))
	}
	if inFence {
		out = append(out, "```")
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func whatsappConvertLine(line string) string {
	trimmed := strings.TrimSpace(line)

	// Headers → bold.
	if rest, ok := strings.CutPrefix(trimmed, "### "); ok {
		return "*" + strings.TrimSpace(rest) + "*"
	}
	if rest, ok := strings.CutPrefix(trimmed, "## "); ok {
		return "*" + strings.TrimSpace(rest) + "*"
	}
	if rest, ok := strings.CutPrefix(trimmed, "# "); ok {
		return "*" + strings.TrimSpace(rest) + "*"
	}

	// Inline conversions via character parsing.
	chars := []rune(trimmed)
	n := len(chars)
	var b strings.Builder
	b.Grow(n * 2)
	i := 0

	for i < n {
		// Inline code: `code` → ```code``` (WhatsApp monospace).
		if chars[i] == '`' && i+1 < n {
			if end, ok := findClosing(chars, i+1, '`'); ok {
				code := string(chars[i+1 : end])
				b.WriteString("```")
				b.WriteString(code)
				b.WriteString("```")
				i = end + 1
				continue
			}
		}

		// Bold: **text** → *text*.
		if i+1 < n && chars[i] == '*' && chars[i+1] == '*' {
			if end, ok := findDoubleClosing(chars, i+2, '*'); ok {
				inner := string(chars[i+2 : end])
				b.WriteByte('*')
				b.WriteString(inner)
				b.WriteByte('*')
				i = end + 2
				continue
			}
		}

		// Markdown link: [text](url) → url (bare link).
		if chars[i] == '[' {
			if _, url, endPos, ok := parseMarkdownLink(chars, i); ok {
				b.WriteString(url)
				i = endPos
				continue
			}
		}

		b.WriteRune(chars[i])
		i++
	}

	return b.String()
}

// --- Signal Formatter ---

// SignalFormatter strips all Markdown (Signal has no rich text support).
// Prefixes output with 🤖 to distinguish agent responses in Notes-to-Self
// threads (Rust parity: format_plain_terminal with robot_prefix=true).
type SignalFormatter struct{}

func (f *SignalFormatter) Platform() string { return "signal" }

func (f *SignalFormatter) Format(content string) string {
	body := formatPlainTerminal(content)
	if body == "" {
		return body
	}
	return "🤖 " + body
}

// formatPlainTerminal converts Markdown to clean plain text for no-markup
// channels. Shared by Signal (with prefix) and Voice (without).
func formatPlainTerminal(content string) string {
	content = preprocess(content)

	var lines []string
	inCode := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue // drop fence markers
		}
		if inCode {
			lines = append(lines, "  "+line)
			continue
		}

		// Strip headers.
		plain := strings.TrimLeft(line, "# ")
		if line != plain {
			plain = strings.TrimSpace(plain)
		}

		// Strip inline formatting.
		plain = strings.ReplaceAll(plain, "**", "")
		plain = strings.ReplaceAll(plain, "__", "")
		plain = strings.ReplaceAll(plain, "~~", "")
		plain = strings.ReplaceAll(plain, "`", "")

		// Strip markdown links: [text](url) → url.
		plain = stripMarkdownLinks(plain)

		lines = append(lines, plain)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// stripMarkdownLinks replaces [text](url) with just url (for channels
// that only support bare links). Uses character-level parsing for
// correctness with nested brackets.
func stripMarkdownLinks(text string) string {
	chars := []rune(text)
	var b strings.Builder
	b.Grow(len(text))
	i := 0
	for i < len(chars) {
		if chars[i] == '[' {
			if _, url, endPos, ok := parseMarkdownLink(chars, i); ok {
				b.WriteString(url)
				i = endPos
				continue
			}
		}
		b.WriteRune(chars[i])
		i++
	}
	return b.String()
}

// --- Email Formatter ---

// EmailFormatter preserves Markdown for HTML-capable email clients.
type EmailFormatter struct{}

func (f *EmailFormatter) Platform() string { return "email" }
func (f *EmailFormatter) Format(content string) string {
	return preprocess(content)
}

// --- Voice Formatter ---

// VoiceFormatter strips all formatting for TTS output. Produces clean
// spoken-word text: no markdown, no code blocks, no URLs.
type VoiceFormatter struct{}

func (f *VoiceFormatter) Platform() string { return "voice" }
func (f *VoiceFormatter) Format(content string) string {
	content = preprocess(content)

	var b strings.Builder
	inCode := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			if !inCode {
				b.WriteString("(end of code)\n")
			} else {
				b.WriteString("(code block)\n")
			}
			continue
		}
		if inCode {
			continue // Skip code block contents for voice.
		}

		// Strip markdown.
		line = strings.ReplaceAll(line, "**", "")
		line = strings.ReplaceAll(line, "__", "")
		line = strings.ReplaceAll(line, "~~", "")
		line = strings.ReplaceAll(line, "`", "")
		for strings.HasPrefix(line, "# ") {
			line = line[2:]
		}

		// Links → just the text, drop URL.
		line = mdLinkRe.ReplaceAllString(line, "$1")

		// Strip bullet markers.
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")

		if trimmed := strings.TrimSpace(line); trimmed != "" {
			b.WriteString(trimmed + " ")
		}
	}
	return strings.TrimSpace(b.String())
}

// --- Matrix Formatter ---

// MatrixFormatter converts Markdown to Matrix's HTML-subset format.
// Matrix supports a subset of HTML in formatted_body.
type MatrixFormatter struct{}

func (f *MatrixFormatter) Platform() string { return "matrix" }
// matrixBoldRe matches **text** for bold conversion.
var matrixBoldRe = regexp.MustCompile(`\*\*([^*]+)\*\*`)

// matrixItalicRe matches *text* for italic conversion (after bold is handled).
var matrixItalicRe = regexp.MustCompile(`\*([^*]+)\*`)

// matrixInlineCodeRe matches `code` for inline code conversion.
var matrixInlineCodeRe = regexp.MustCompile("`([^`]+)`")

func (f *MatrixFormatter) Format(content string) string {
	content = preprocess(content)

	var b strings.Builder
	inCode := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "```") {
			if inCode {
				b.WriteString("</code></pre>\n")
			} else {
				lang := strings.TrimPrefix(line, "```")
				if lang != "" {
					b.WriteString("<pre><code class=\"language-" + lang + "\">")
				} else {
					b.WriteString("<pre><code>")
				}
			}
			inCode = !inCode
			continue
		}
		if inCode {
			b.WriteString(line + "\n")
			continue
		}

		// Headers -> HTML.
		if strings.HasPrefix(line, "### ") {
			b.WriteString("<h3>" + strings.TrimPrefix(line, "### ") + "</h3>\n")
			continue
		}
		if strings.HasPrefix(line, "## ") {
			b.WriteString("<h2>" + strings.TrimPrefix(line, "## ") + "</h2>\n")
			continue
		}
		if strings.HasPrefix(line, "# ") {
			b.WriteString("<h1>" + strings.TrimPrefix(line, "# ") + "</h1>\n")
			continue
		}

		// Inline formatting: bold, italic, code, links.
		line = matrixBoldRe.ReplaceAllString(line, "<strong>$1</strong>")
		line = matrixItalicRe.ReplaceAllString(line, "<em>$1</em>")
		line = matrixInlineCodeRe.ReplaceAllString(line, "<code>$1</code>")
		line = mdLinkRe.ReplaceAllString(line, `<a href="$2">$1</a>`)

		b.WriteString(line + "\n")
	}
	return strings.TrimSpace(b.String())
}

// --- Web Formatter ---

// WebFormatter preserves Markdown for client-side rendering.
type WebFormatter struct{}

func (f *WebFormatter) Platform() string { return "web" }
func (f *WebFormatter) Format(content string) string {
	return preprocess(content)
}
