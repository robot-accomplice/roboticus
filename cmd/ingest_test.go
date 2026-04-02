package cmd

import (
	"strings"
	"testing"
)

func TestChunkText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxChars int
		wantMin  int // minimum chunks
		wantMax  int // maximum chunks
	}{
		{"short text", "hello world", 100, 1, 1},
		{"exact boundary", "hello", 5, 1, 1},
		{"needs splitting", "para one\n\npara two\n\npara three", 15, 2, 3},
		{"empty text", "", 100, 0, 1},
		{"single long paragraph", strings.Repeat("x", 200), 50, 1, 1}, // no paragraph breaks
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := chunkText(tt.text, tt.maxChars)
			if len(chunks) < tt.wantMin {
				t.Errorf("got %d chunks, want >= %d", len(chunks), tt.wantMin)
			}
			if len(chunks) > tt.wantMax {
				t.Errorf("got %d chunks, want <= %d", len(chunks), tt.wantMax)
			}
		})
	}
}
