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
	g := &ContentGuard{}
	tests := []struct {
		input string
		want  string
	}{
		{"safe content", "safe content"},
		{"has [SYSTEM] marker", "has  marker"},
		{"has <|im_start|> tag", "has  tag"},
		{"multiple [INST] and [/INST]", "multiple  and "},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := g.Transform(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
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
	input := "<think>reasoning</think>\n\n\n\n\nHello [SYSTEM] world  "
	got := p.Apply(input)
	want := "Hello  world"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
