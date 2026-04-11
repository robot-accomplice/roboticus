package llm

import "testing"

func TestReasoningExtractor(t *testing.T) {
	r := &ReasoningExtractor{}
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no think block", "Hello world", "Hello world"},
		{"single think block", "<think>reasoning here</think>Answer", "Answer"},
		{"multiline think", "<think>\nstep 1\nstep 2\n</think>\nFinal answer", "Final answer"},
		{"multiple blocks", "<think>a</think>Middle<think>b</think>End", "MiddleEnd"},
		{"empty after strip", "<think>only reasoning</think>", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Transform(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContentGuard(t *testing.T) {
	// Rust parity: ContentGuard replaces ENTIRE content with filtered message
	// when any injection marker is detected. Does NOT strip markers individually.
	g := &ContentGuard{}
	tests := []struct {
		name    string
		input   string
		want    string
		flagged bool
	}{
		{"safe content", "safe content", "safe content", false},
		{"[SYSTEM] marker", "has [SYSTEM] marker", filteredMessage, true},
		{"[INST] marker", "something [INST] malicious", filteredMessage, true},
		{"<|im_start|> tag", "has <|im_start|> tag", filteredMessage, true},
		{"<s> marker", "text with <s> inside", filteredMessage, true},
		{"</s> marker", "text with </s> inside", filteredMessage, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := g.Transform(tt.input)
			if got != tt.want {
				t.Errorf("Transform() = %q, want %q", got, tt.want)
			}
			if g.HasInjection(tt.input) != tt.flagged {
				t.Errorf("HasInjection() = %v, want %v", g.HasInjection(tt.input), tt.flagged)
			}
		})
	}
}

func TestFormatNormalizer(t *testing.T) {
	f := &FormatNormalizer{}
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"trims whitespace", "  hello  ", "hello"},
		{"collapses newlines", "a\n\n\n\n\nb", "a\n\nb"},
		{"preserves double newline", "a\n\nb", "a\n\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := f.Transform(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTransformPipeline(t *testing.T) {
	p := DefaultTransformPipeline()
	// Safe content: reasoning stripped, whitespace normalized.
	safe := "<think>reasoning</think>\n\n\n\n\nHello world  "
	got := p.Apply(safe)
	want := "Hello world"
	if got != want {
		t.Errorf("safe: got %q, want %q", got, want)
	}

	// Injection content: entire response replaced (Rust parity).
	injected := "<think>reasoning</think>\n\nHello [SYSTEM] world"
	got2 := p.Apply(injected)
	if got2 != filteredMessage {
		t.Errorf("injected: got %q, want %q", got2, filteredMessage)
	}
}
