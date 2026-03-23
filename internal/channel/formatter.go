package channel

import (
	"regexp"
	"strings"
)

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
type TelegramFormatter struct{}

func (f *TelegramFormatter) Platform() string { return "telegram" }

var telegramEscapeChars = strings.NewReplacer(
	"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]",
	"(", "\\(", ")", "\\)", "~", "\\~", "`", "\\`",
	">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-",
	"=", "\\=", "|", "\\|", "{", "\\{", "}", "\\}",
	".", "\\.", "!", "\\!",
)

func (f *TelegramFormatter) Format(content string) string {
	content = preprocess(content)

	// Process line by line, preserving code blocks.
	var b strings.Builder
	inCode := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		if inCode {
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}

		// Headers → bold.
		if strings.HasPrefix(line, "# ") {
			b.WriteString("*")
			b.WriteString(telegramEscapeChars.Replace(strings.TrimPrefix(line, "# ")))
			b.WriteString("*\n")
			continue
		}
		if strings.HasPrefix(line, "## ") {
			b.WriteString("*")
			b.WriteString(telegramEscapeChars.Replace(strings.TrimPrefix(line, "## ")))
			b.WriteString("*\n")
			continue
		}

		b.WriteString(telegramEscapeChars.Replace(line))
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
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
type WhatsAppFormatter struct{}

func (f *WhatsAppFormatter) Platform() string { return "whatsapp" }

var mdLinkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

func (f *WhatsAppFormatter) Format(content string) string {
	content = preprocess(content)

	// **bold** → *bold*
	content = strings.ReplaceAll(content, "**", "*")

	// Headers → bold.
	lines := strings.Split(content, "\n")
	var b strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			b.WriteString("*" + strings.TrimPrefix(line, "# ") + "*\n")
		} else if strings.HasPrefix(line, "## ") {
			b.WriteString("*" + strings.TrimPrefix(line, "## ") + "*\n")
		} else {
			b.WriteString(line + "\n")
		}
	}
	content = b.String()

	// [text](url) → url (WhatsApp doesn't support link syntax).
	content = mdLinkRe.ReplaceAllString(content, "$2")

	return strings.TrimSpace(content)
}

// --- Signal Formatter ---

// SignalFormatter strips all Markdown (Signal has no rich text support).
type SignalFormatter struct{}

func (f *SignalFormatter) Platform() string { return "signal" }

func (f *SignalFormatter) Format(content string) string {
	content = preprocess(content)

	var b strings.Builder
	inCode := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			b.WriteString("  " + line + "\n")
			continue
		}

		// Strip markdown characters.
		line = strings.ReplaceAll(line, "**", "")
		line = strings.ReplaceAll(line, "__", "")
		line = strings.ReplaceAll(line, "~~", "")
		line = strings.ReplaceAll(line, "`", "")

		// Strip headers.
		for strings.HasPrefix(line, "# ") {
			line = line[2:]
		}

		// Extract URLs from links.
		line = mdLinkRe.ReplaceAllString(line, "$2")

		b.WriteString(line + "\n")
	}
	return strings.TrimSpace(b.String())
}

// --- Email Formatter ---

// EmailFormatter preserves Markdown for HTML-capable email clients.
type EmailFormatter struct{}

func (f *EmailFormatter) Platform() string { return "email" }
func (f *EmailFormatter) Format(content string) string {
	return preprocess(content)
}

// --- Web Formatter ---

// WebFormatter preserves Markdown for client-side rendering.
type WebFormatter struct{}

func (f *WebFormatter) Platform() string { return "web" }
func (f *WebFormatter) Format(content string) string {
	return preprocess(content)
}
