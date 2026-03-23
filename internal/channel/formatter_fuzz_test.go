package channel

import "testing"

func FuzzTelegramFormatter(f *testing.F) {
	formatter := &TelegramFormatter{}

	f.Add("Hello world")
	f.Add("# Header\n## Subheader\nText")
	f.Add("```go\nfunc main() {}\n```")
	f.Add("**bold** _italic_ `code`")
	f.Add("[link](https://example.com)")
	f.Add("")
	f.Add("Special chars: _*[]()~`>#+-=|{}.!")

	f.Fuzz(func(t *testing.T, input string) {
		// Must not panic.
		_ = formatter.Format(input)
	})
}

func FuzzSignalFormatter(f *testing.F) {
	formatter := &SignalFormatter{}

	f.Add("Hello world")
	f.Add("**bold** and `code`")
	f.Add("```\ncode block\n```")
	f.Add("[link](https://example.com)")
	f.Add("")

	f.Fuzz(func(t *testing.T, input string) {
		_ = formatter.Format(input)
	})
}

func FuzzWhatsAppFormatter(f *testing.F) {
	formatter := &WhatsAppFormatter{}

	f.Add("**bold** text")
	f.Add("# Header")
	f.Add("[link](https://example.com)")
	f.Add("")

	f.Fuzz(func(t *testing.T, input string) {
		_ = formatter.Format(input)
	})
}
