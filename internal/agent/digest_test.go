package agent

import (
	"context"
	"errors"
	"testing"

	"goboticus/internal/llm"
)

func TestDigestGenerator_Empty(t *testing.T) {
	dg := NewDigestGenerator(nil)
	result, err := dg.GenerateDigest(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No messages to summarize." {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestDigestGenerator_WithSummarizer(t *testing.T) {
	ms := &mockSummarizer{summarizeFunc: func(_ context.Context, _ []llm.Message) (string, error) {
		return "test summary", nil
	}}
	dg := NewDigestGenerator(ms)
	msgs := []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	result, err := dg.GenerateDigest(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "test summary" {
		t.Errorf("expected 'test summary', got %q", result)
	}
}

func TestDigestGenerator_SummarizerError(t *testing.T) {
	wantErr := errors.New("summarizer failed")
	ms := &mockSummarizer{summarizeFunc: func(_ context.Context, _ []llm.Message) (string, error) {
		return "", wantErr
	}}
	dg := NewDigestGenerator(ms)
	msgs := []llm.Message{{Role: "user", Content: "hello"}}
	_, err := dg.GenerateDigest(context.Background(), msgs)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDigestGenerator_FallbackWithoutSummarizer(t *testing.T) {
	dg := NewDigestGenerator(nil)
	msgs := []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "user", Content: "how are you"},
		{Role: "assistant", Content: "I'm fine"},
		{Role: "tool", Content: "result"},
	}
	result, err := dg.GenerateDigest(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Session digest: 2 user messages, 1 assistant responses, 1 tool calls."
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestDigestGenerator_ShouldDigest(t *testing.T) {
	dg := NewDigestGenerator(nil)

	// Should trigger at multiples of interval
	if !dg.ShouldDigest(20, 20) {
		t.Error("expected ShouldDigest(20, 20) = true")
	}
	if !dg.ShouldDigest(40, 20) {
		t.Error("expected ShouldDigest(40, 20) = true")
	}
	if dg.ShouldDigest(21, 20) {
		t.Error("expected ShouldDigest(21, 20) = false")
	}
	if dg.ShouldDigest(0, 20) {
		t.Error("expected ShouldDigest(0, 20) = false")
	}

	// Default interval of 20 when <= 0
	if !dg.ShouldDigest(20, 0) {
		t.Error("expected ShouldDigest(20, 0) = true with default interval 20")
	}
}
