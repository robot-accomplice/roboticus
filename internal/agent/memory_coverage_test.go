package agent

import (
	"context"
	"testing"

	"goboticus/testutil"
)

func TestMemoryManager_IngestTurn(t *testing.T) {
	store := testutil.TempStore(t)
	mgr := NewMemoryManager(MemoryConfig{TotalTokenBudget: 2048}, store)
	if mgr == nil {
		t.Fatal("nil")
	}

	sess := NewSession("s1", "a1", "bot")
	sess.AddUserMessage("What is the capital of France?")
	sess.AddAssistantMessage("Paris is the capital of France.", nil)

	// IngestTurn should not panic or error.
	mgr.IngestTurn(context.Background(), sess)
}

func TestMemoryRetriever_Retrieve(t *testing.T) {
	store := testutil.TempStore(t)
	cfg := DefaultRetrievalConfig()
	budgets := MemoryTierBudget{
		Working: 0.3, Episodic: 0.2, Semantic: 0.2,
		Procedural: 0.15, Relationship: 0.15,
	}
	retriever := NewMemoryRetriever(cfg, budgets, store)
	if retriever == nil {
		t.Fatal("nil")
	}

	// Retrieve with no data — should return empty.
	block := retriever.Retrieve(context.Background(), "s1", "test query", 1000)
	// May return empty or formatted block, just don't panic.
	_ = block
}

func TestDefaultRetrievalConfig(t *testing.T) {
	cfg := DefaultRetrievalConfig()
	if cfg.HybridWeight < 0 || cfg.HybridWeight > 1 {
		t.Errorf("hybrid weight out of range: %f", cfg.HybridWeight)
	}
	if cfg.EpisodicHalfLife <= 0 {
		t.Error("episodic half life should be positive")
	}
}
