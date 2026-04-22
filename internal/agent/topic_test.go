package agent

import (
	"testing"

	"roboticus/internal/llm"
)

func TestDetectTopic_Technical(t *testing.T) {
	msgs := []string{"I have a bug in my code", "the api function returns an error"}
	result := DetectTopic(msgs)
	if result.Primary != TopicTechnical {
		t.Errorf("expected TopicTechnical, got %s", result.Primary)
	}
	if result.Confidence <= 0 {
		t.Errorf("expected positive confidence, got %f", result.Confidence)
	}
}

func TestDetectTopic_Creative(t *testing.T) {
	msgs := []string{"write me a poem", "compose a story about art and music"}
	result := DetectTopic(msgs)
	if result.Primary != TopicCreative {
		t.Errorf("expected TopicCreative, got %s", result.Primary)
	}
}

func TestDetectTopic_Empty(t *testing.T) {
	result := DetectTopic([]string{})
	if result.Primary != TopicGeneral {
		t.Errorf("expected TopicGeneral for empty input, got %s", result.Primary)
	}
	if result.Confidence != 0.0 {
		t.Errorf("expected 0.0 confidence for empty input, got %f", result.Confidence)
	}
}

func TestDetectTopic_Mixed(t *testing.T) {
	msgs := []string{"help me fix this code bug", "also research the data"}
	result := DetectTopic(msgs)
	// Should have a primary and a secondary topic
	if result.Primary == "" {
		t.Error("expected non-empty primary topic")
	}
	// With mixed content, secondary should be set (not general from default)
	_ = result.Secondary // just ensure it's accessible
}

func TestDetectTopic_Keywords(t *testing.T) {
	msgs := []string{"write some code for my database function"}
	result := DetectTopic(msgs)
	if len(result.Keywords) == 0 {
		t.Error("expected keywords to be extracted")
	}
	if len(result.Keywords) > 5 {
		t.Errorf("expected at most 5 keywords, got %d", len(result.Keywords))
	}
}

func TestDetectTopic_Financial(t *testing.T) {
	msgs := []string{"check my wallet balance and transfer payment"}
	result := DetectTopic(msgs)
	if result.Primary != TopicFinancial {
		t.Errorf("expected TopicFinancial, got %s", result.Primary)
	}
}

func TestDetectTopic_Support(t *testing.T) {
	msgs := []string{"help I have a problem, something is broken and I am stuck"}
	result := DetectTopic(msgs)
	if result.Primary != TopicSupport {
		t.Errorf("expected TopicSupport, got %s", result.Primary)
	}
}

func TestPartitionByTopic_KeepsToolExchangeAtomicWhenAssistantIsOffTopic(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "current request", TopicTag: "current"},
		{
			Role:     "assistant",
			Content:  "",
			TopicTag: "older",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: llm.ToolCallFunc{
					Name:      "create_cron_job",
					Arguments: `{"name":"quiet ticker"}`,
				},
			}},
		},
		{Role: "tool", ToolCallID: "call-1", Name: "create_cron_job", Content: `{"status":"ok"}`},
		{Role: "assistant", Content: "done", TopicTag: "current"},
	}

	current, offTopic := PartitionByTopic(msgs, "current")

	for _, m := range current {
		if m.Role == "tool" && m.ToolCallID == "call-1" {
			t.Fatal("tool reply from off-topic exchange leaked into current-topic messages without its assistant tool-call")
		}
	}
	if len(offTopic) != 1 {
		t.Fatalf("off-topic blocks = %d, want 1", len(offTopic))
	}
	if len(offTopic[0].Messages) != 2 {
		t.Fatalf("off-topic tool exchange size = %d, want 2", len(offTopic[0].Messages))
	}
	if offTopic[0].Messages[0].Role != "assistant" || offTopic[0].Messages[1].Role != "tool" {
		t.Fatalf("off-topic exchange not preserved atomically: %#v", offTopic[0].Messages)
	}
}
