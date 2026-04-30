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

func TestContextBuilder_BuildRequestWithTrailingSystemOverlay_LayersAfterHistory(t *testing.T) {
	cb := NewContextBuilder(DefaultContextConfig())
	cb.SetSystemPrompt("base system prompt")
	sess := NewSession("ctx-overlay-sess", "agent-1", "TestBot")
	sess.AddUserMessage("user asks something")
	sess.AddAssistantMessage("assistant replies", nil)

	req := cb.BuildRequestWithTrailingSystemOverlay(sess, []string{"overlay note A", "overlay note B"})
	msgs := req.Messages
	if len(msgs) < 4 {
		t.Fatalf("len(messages)=%d, want at least base system + user + assistant + 2 overlay", len(msgs))
	}
	n := len(msgs)
	if msgs[n-2].Role != "system" || msgs[n-2].Content != "overlay note A" {
		t.Fatalf("penultimate msg role=%q content=%q", msgs[n-2].Role, msgs[n-2].Content)
	}
	if msgs[n-1].Role != "system" || msgs[n-1].Content != "overlay note B" {
		t.Fatalf("last msg role=%q content=%q", msgs[n-1].Role, msgs[n-1].Content)
	}
	userIdx, asstIdx := -1, -1
	for i, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, "user asks something") {
			userIdx = i
		}
		if m.Role == "assistant" && m.Content == "assistant replies" {
			asstIdx = i
		}
	}
	if userIdx < 0 || asstIdx < 0 {
		t.Fatalf("history not found: userIdx=%d asstIdx=%d", userIdx, asstIdx)
	}
	if asstIdx >= n-2 {
		t.Fatalf("conversation history must precede trailing overlay (assistant at %d, overlay starts at %d)", asstIdx, n-2)
	}
}

func TestContextBuilder_TrailingOverlayPreservesWorkingMemory(t *testing.T) {
	cb := NewContextBuilder(DefaultContextConfig())
	cb.SetSystemPrompt("base system prompt")
	cb.SetMemory("[Active Memory]\n\n[Working State]\n- Release focus: preserve durable continuity")
	cb.SetMemoryIndex("[Memory Index]\n- recall_memory(\"abc123\") release continuity note")

	sess := NewSession("ctx-overlay-memory", "agent-1", "TestBot")
	sess.AddUserMessage("continue from our prior release plan")
	sess.AddAssistantMessage("I will preserve the plan context.", nil)

	req := cb.BuildRequestWithTrailingSystemOverlay(sess, []string{"TOTOF reflection brief"})

	var memoryIdx, indexIdx, userIdx, overlayIdx = -1, -1, -1, -1
	for i, msg := range req.Messages {
		if msg.Role != "system" && msg.Role != "user" {
			continue
		}
		if strings.Contains(msg.Content, "[Working State]") {
			memoryIdx = i
		}
		if strings.Contains(msg.Content, "[Memory Index]") {
			indexIdx = i
		}
		if msg.Role == "user" && strings.Contains(msg.Content, "prior release plan") {
			userIdx = i
		}
		if msg.Role == "system" && msg.Content == "TOTOF reflection brief" {
			overlayIdx = i
		}
	}
	if memoryIdx < 0 {
		t.Fatal("trailing overlay request dropped active working memory (R-AGENT-202)")
	}
	if indexIdx < 0 {
		t.Fatal("trailing overlay request dropped memory index (R-AGENT-202)")
	}
	if userIdx < 0 {
		t.Fatal("trailing overlay request dropped conversation history")
	}
	if overlayIdx != len(req.Messages)-1 {
		t.Fatalf("overlay index = %d, want last index %d", overlayIdx, len(req.Messages)-1)
	}
	if memoryIdx >= userIdx || indexIdx >= userIdx || userIdx >= overlayIdx {
		t.Fatalf("unexpected order: memory=%d index=%d user=%d overlay=%d", memoryIdx, indexIdx, userIdx, overlayIdx)
	}
}
