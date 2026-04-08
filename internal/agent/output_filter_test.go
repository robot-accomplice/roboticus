package agent

import (
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"plain text", "plain text"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"\x1b[1;32mbold green\x1b[0m", "bold green"},
		{"line1\x1b[2Kline2", "line1line2"},
	}
	for _, tt := range tests {
		got := stripANSI(tt.input)
		if got != tt.want {
			t.Errorf("stripANSI(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFilterProgressLines(t *testing.T) {
	input := "Compiling...\n████████████████████░░░░ 80%\nDone."
	got := filterProgressLines(input)
	if strings.Contains(got, "████") {
		t.Error("should remove progress bar line")
	}
	if !strings.Contains(got, "Compiling...") {
		t.Error("should keep non-progress lines")
	}
	if !strings.Contains(got, "Done.") {
		t.Error("should keep non-progress lines")
	}
}

func TestDedupConsecutiveLines(t *testing.T) {
	input := "line1\nline2\nline2\nline2\nline3"
	got := dedupConsecutiveLines(input)
	want := "line1\nline2\nline3"
	if got != want {
		t.Errorf("dedupConsecutiveLines = %q, want %q", got, want)
	}
}

func TestNormalizeWhitespace(t *testing.T) {
	input := "line1\n\n\n\n\nline2\n  trailing  \n"
	got := normalizeWhitespace(input)
	// 5 blank lines between line1 and line2 should collapse to at most 2.
	// That means: "line1\n\n\nline2" (2 blank lines = 3 newlines max between content).
	blankRun := 0
	maxRun := 0
	for _, c := range got {
		if c == '\n' {
			blankRun++
			if blankRun > maxRun {
				maxRun = blankRun
			}
		} else {
			blankRun = 0
		}
	}
	if maxRun > 3 { // 3 newlines = 2 blank lines
		t.Errorf("should collapse >2 blank lines, got max run of %d newlines", maxRun)
	}
	if strings.Contains(got, "  trailing  ") {
		t.Error("should trim trailing whitespace")
	}
}

func TestFilterToolOutput_Integration(t *testing.T) {
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
