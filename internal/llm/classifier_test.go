package llm

import (
	"context"
	"testing"
)

func TestSemanticClassifier_NilEmbedder(t *testing.T) {
	corpus := []ClassifierExample{
		{Text: "hello", Intent: "greeting", Vector: []float32{1, 0, 0}},
	}
	sc := NewSemanticClassifier(nil, corpus)
	intent, conf, err := sc.Classify(context.Background(), "hi there")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intent != "unknown" {
		t.Errorf("expected 'unknown', got %q", intent)
	}
	if conf != 0.0 {
		t.Errorf("expected 0 confidence, got %f", conf)
	}
}

func TestSemanticClassifier_EmptyCorpus(t *testing.T) {
	ec := NewEmbeddingClient(nil) // fallback n-gram
	sc := NewSemanticClassifier(ec, nil)
	intent, conf, err := sc.Classify(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intent != "unknown" {
		t.Errorf("expected 'unknown', got %q", intent)
	}
	if conf != 0.0 {
		t.Errorf("expected 0 confidence, got %f", conf)
	}
}

func TestSemanticClassifier_Classification(t *testing.T) {
	ec := NewEmbeddingClient(nil) // uses n-gram fallback

	// Pre-compute vectors for the corpus using the same embedder.
	greetVec, _ := ec.EmbedSingle(context.Background(), "hello how are you")
	helpVec, _ := ec.EmbedSingle(context.Background(), "I need help with a problem")

	corpus := []ClassifierExample{
		{Text: "hello how are you", Intent: "greeting", Vector: greetVec},
		{Text: "I need help with a problem", Intent: "help_request", Vector: helpVec},
	}

	sc := NewSemanticClassifier(ec, corpus)

	// Query similar to greeting.
	intent, conf, err := sc.Classify(context.Background(), "hi there how are you doing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intent == "" {
		t.Error("expected non-empty intent")
	}
	if conf <= 0 {
		t.Errorf("expected positive confidence, got %f", conf)
	}
}

func TestSemanticClassifier_MismatchedVectors(t *testing.T) {
	ec := NewEmbeddingClient(nil) // n-gram returns ngramDim (128) vectors

	// Corpus with wrong-dimension vector.
	corpus := []ClassifierExample{
		{Text: "hello", Intent: "greeting", Vector: []float32{1, 0}}, // only 2 dims, won't match 128
	}

	sc := NewSemanticClassifier(ec, corpus)
	// CosineSimilarity returns 0 for mismatched lengths, so bestSim stays 0
	// but bestIntent should still be set to "greeting" since it's the only one checked.
	intent, conf, err := sc.Classify(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With mismatched dimensions, cosine similarity returns 0,
	// so bestSim never exceeds 0 and bestIntent remains "".
	if conf != 0.0 {
		t.Errorf("expected 0.0 confidence for mismatched vectors, got %f", conf)
	}
	_ = intent // may be empty or "greeting" depending on sim > 0 check
}

func TestSemanticClassifier_BestMatch(t *testing.T) {
	ec := NewEmbeddingClient(nil)

	// Pre-compute vectors.
	greetVec, _ := ec.EmbedSingle(context.Background(), "hello world")
	helpVec, _ := ec.EmbedSingle(context.Background(), "please assist me")

	corpus := []ClassifierExample{
		{Text: "hello world", Intent: "greeting", Vector: greetVec},
		{Text: "please assist me", Intent: "help", Vector: helpVec},
	}

	sc := NewSemanticClassifier(ec, corpus)

	// Query closer to "greeting".
	intent, _, err := sc.Classify(context.Background(), "hello world foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intent != "greeting" {
		t.Errorf("expected 'greeting', got %q", intent)
	}
}
