package agent

import (
	"strings"
	"testing"
)

func TestFilterToolOutput_StripsNoiseAndPreservesSignal(t *testing.T) {
	input := "\x1b[32mOK\x1b[0m\nline1\nline1\n████████ 95%\nresult"
	got := FilterToolOutput(input)
	if strings.Contains(got, "\x1b") {
		t.Error("ANSI not stripped")
	}
	if strings.Count(got, "line1") != 1 {
		t.Error("duplicate line not removed")
	}
	if strings.Contains(got, "████") {
		t.Error("progress line not removed")
	}
	if !strings.Contains(got, "result") {
		t.Error("result line should remain")
	}
}

func TestFilterToolOutput_CollapsesWhitespace(t *testing.T) {
	input := "line1\n\n\n\n\nline2\n  trailing  \n"
	got := FilterToolOutput(input)
	if strings.Contains(got, "  trailing  ") {
		t.Error("should trim trailing whitespace")
	}
	if strings.Contains(got, "\n\n\n\n") {
		t.Errorf("should collapse excessive blank lines, got %q", got)
	}
}
