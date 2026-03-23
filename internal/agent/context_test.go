package agent

import (
	"strings"
	"testing"
)

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		input string
		count int
	}{
		{"Hello world.", 1},
		{"Hello. World.", 2},
		{"What? Yes! OK.", 3},
		{"No punctuation", 1},
		{"", 0},
		{"Multiple. Sentences. In a row. Here.", 4},
		{"Dr. Smith is here.", 2}, // "Dr." splits (simple algorithm, no abbreviation awareness)
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitSentences(tt.input)
			if len(got) != tt.count {
				t.Errorf("splitSentences(%q) = %d sentences %v, want %d", tt.input, len(got), got, tt.count)
			}
		})
	}
}

func TestSemanticCompress_ShortContent(t *testing.T) {
	// Content with ≤2 sentences should be returned as-is.
	content := "Short response."
	got := semanticCompress(content)
	if got != content {
		t.Errorf("short content: got %q, want %q", got, content)
	}
}

func TestSemanticCompress_ReducesLength(t *testing.T) {
	// Long content with many sentences should be compressed.
	sentences := []string{
		"The system architecture uses a pipeline pattern.",
		"Each stage processes the input independently.",
		"Working memory stores active session context.",
		"Episodic memory tracks past events with decay.",
		"Semantic memory holds structured knowledge.",
		"Procedural memory records tool usage statistics.",
		"Relationship memory tracks entity interactions.",
		"The guard chain validates all output before delivery.",
	}
	content := strings.Join(sentences, " ")
	got := semanticCompress(content)
	if len(got) >= len(content) {
		t.Errorf("compressed length %d >= original %d", len(got), len(content))
	}
	if len(got) == 0 {
		t.Error("compressed content should not be empty")
	}
}

func TestSemanticCompress_PreservesFirstSentence(t *testing.T) {
	content := "The main conclusion is important. " +
		"Some filler content here. More filler. " +
		"Even more filler text. And yet more filler. " +
		"Additional padding. Extra content. The end."
	got := semanticCompress(content)
	// First sentence should be preserved (positional bonus).
	if !strings.Contains(got, "main conclusion") {
		t.Errorf("first sentence not preserved: %q", got)
	}
}

func TestContainsNumber(t *testing.T) {
	if !containsNumber("abc123") {
		t.Error("should detect numbers in abc123")
	}
	if containsNumber("abcdef") {
		t.Error("should not detect numbers in abcdef")
	}
	if containsNumber("") {
		t.Error("empty string should not contain numbers")
	}
}

func TestStageFromExcess(t *testing.T) {
	tests := []struct {
		ratio float64
		want  CompactionStage
	}{
		{0.5, StageVerbatim},
		{1.0, StageVerbatim},
		{1.2, StageSelectiveTrim},
		{1.5, StageSelectiveTrim},
		{2.0, StageSemanticCompress},
		{2.5, StageSemanticCompress},
		{3.5, StageTopicExtract},
		{4.0, StageTopicExtract},
		{5.0, StageSkeleton},
		{10.0, StageSkeleton},
	}

	for _, tt := range tests {
		got := stageFromExcess(tt.ratio)
		if got != tt.want {
			t.Errorf("stageFromExcess(%f) = %v, want %v", tt.ratio, got, tt.want)
		}
	}
}

func TestIsSocialFiller(t *testing.T) {
	fillers := []string{"ok", "thanks", "hello", "hi", "hey", "sure", "yes", "no", "okay"}
	for _, f := range fillers {
		if !isSocialFiller(f) {
			t.Errorf("isSocialFiller(%q) = false, want true", f)
		}
	}

	nonFillers := []string{"Can you help me with this code?", "What is the weather today?", ""}
	for _, nf := range nonFillers {
		if isSocialFiller(nf) {
			t.Errorf("isSocialFiller(%q) = true, want false", nf)
		}
	}
}
