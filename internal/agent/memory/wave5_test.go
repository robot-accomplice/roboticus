package memory

import (
	"context"
	"testing"
	"time"

	"roboticus/internal/db"
	"roboticus/testutil"
)

// --- ANN Retrieval (#53) ---

func TestRetrieveWithANN_NoIndex(t *testing.T) {
	store := testutil.TempStore(t)
	r := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)

	// No vector index attached — should return nil.
	results := r.RetrieveWithANN(context.Background(), []float64{1, 0, 0}, 5)
	if results != nil {
		t.Errorf("expected nil results without vector index, got %d", len(results))
	}
}

func TestRetrieveWithANN_WithIndex(t *testing.T) {
	store := testutil.TempStore(t)
	r := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)

	idx := db.NewBruteForceIndex(db.VectorIndexConfig{MinEntries: 1})
	idx.AddEntry(db.VectorEntry{
		SourceTable:    "episodic",
		SourceID:       "ep1",
		ContentPreview: "the sky is blue",
		Embedding:      []float64{1, 0, 0},
	})
	idx.AddEntry(db.VectorEntry{
		SourceTable:    "semantic",
		SourceID:       "sem1",
		ContentPreview: "grass is green",
		Embedding:      []float64{0, 1, 0},
	})

	r.SetVectorIndex(idx)

	results := r.RetrieveWithANN(context.Background(), []float64{1, 0, 0}, 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// First result should be most similar (same vector).
	if results[0].SourceID != "ep1" {
		t.Errorf("expected first result to be ep1, got %s", results[0].SourceID)
	}
	if results[0].Similarity < 0.99 {
		t.Errorf("expected high similarity for exact match, got %f", results[0].Similarity)
	}
}

func TestRetrieveWithANN_ZeroK(t *testing.T) {
	store := testutil.TempStore(t)
	r := NewRetriever(DefaultRetrievalConfig(), DefaultTierBudget(), store)
	idx := db.NewBruteForceIndex(db.VectorIndexConfig{MinEntries: 1})
	idx.AddEntry(db.VectorEntry{
		SourceTable: "episodic", SourceID: "ep1",
		ContentPreview: "test", Embedding: []float64{1, 0},
	})
	r.SetVectorIndex(idx)

	results := r.RetrieveWithANN(context.Background(), []float64{1, 0}, 0)
	if results != nil {
		t.Errorf("expected nil for k=0, got %d results", len(results))
	}
}

// --- Memory Index (#54) ---

func TestMemoryIndex_LookupEmpty(t *testing.T) {
	store := testutil.TempStore(t)
	mi := NewMemoryIndex(store)

	results := mi.Lookup("anything", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results from empty index, got %d", len(results))
	}
}

func TestMemoryIndex_LoadAndLookup(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Seed memory_index entries.
	_, err := store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, confidence)
		 VALUES ('mi1', 'episodic_memory', 'ep1', 'the weather is sunny today', 0.9)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, confidence)
		 VALUES ('mi2', 'semantic_memory', 'sem1', 'database query optimization', 0.7)`)
	if err != nil {
		t.Fatal(err)
	}

	mi := NewMemoryIndex(store)
	if err := mi.Load(ctx); err != nil {
		t.Fatal(err)
	}

	if mi.EntryCount() != 2 {
		t.Errorf("expected 2 entries, got %d", mi.EntryCount())
	}

	// Lookup by keyword.
	results := mi.Lookup("weather", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 match for 'weather', got %d", len(results))
	}
	if results[0].Tier != TierEpisodic {
		t.Errorf("expected episodic tier, got %s", results[0].Tier)
	}

	// Lookup with limit.
	results = mi.Lookup("the", 1)
	if len(results) != 1 {
		t.Errorf("expected 1 result with limit=1, got %d", len(results))
	}
}

func TestMemoryTier_String(t *testing.T) {
	tests := []struct {
		tier MemoryTier
		want string
	}{
		{TierWorking, "working"},
		{TierEpisodic, "episodic"},
		{TierSemantic, "semantic"},
		{TierProcedural, "procedural"},
		{TierRelationship, "relationship"},
	}
	for _, tt := range tests {
		if got := tt.tier.String(); got != tt.want {
			t.Errorf("MemoryTier(%d).String() = %q, want %q", tt.tier, got, tt.want)
		}
	}
}

func TestMemoryTierFromString(t *testing.T) {
	if got := MemoryTierFromString("semantic"); got != TierSemantic {
		t.Errorf("MemoryTierFromString('semantic') = %d, want %d", got, TierSemantic)
	}
	if got := MemoryTierFromString("WORKING"); got != TierWorking {
		t.Errorf("MemoryTierFromString('WORKING') = %d, want %d", got, TierWorking)
	}
	if got := MemoryTierFromString("unknown"); got != TierEpisodic {
		t.Errorf("MemoryTierFromString('unknown') should default to episodic, got %d", got)
	}
}

// --- Phase 0: Mark Derivable Stale (#55) ---

func TestPhase0_MarkDerivableStale(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()

	// Seed a derivable tool output episodic entry.
	seedEpisodic(t, store, "ep-derivable", "tool_event", "list_directory: found 5 files", 5, nowStr())
	// Seed a non-derivable tool output.
	seedEpisodic(t, store, "ep-other", "tool_event", "custom_tool: did something", 5, nowStr())

	marked := pipe.Phase0_MarkDerivableStale(ctx, store)
	if marked != 1 {
		t.Errorf("expected 1 marked stale, got %d", marked)
	}

	var state string
	_ = store.QueryRowContext(ctx, `SELECT memory_state FROM episodic_memory WHERE id = 'ep-derivable'`).Scan(&state)
	if state != "stale" {
		t.Errorf("expected derivable entry state 'stale', got %q", state)
	}

	_ = store.QueryRowContext(ctx, `SELECT memory_state FROM episodic_memory WHERE id = 'ep-other'`).Scan(&state)
	if state != "active" {
		t.Errorf("expected non-derivable entry state 'active', got %q", state)
	}
}

// --- Phase 2: Obsidian Vault Scan (#56) ---

func TestPhase2_ObsidianVaultScan_CleansStaleEntries(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()

	// Seed an obsidian index entry that hasn't been verified in 10 days.
	_, err := store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, confidence, last_verified)
		 VALUES ('obs1', 'obsidian', 'vault-note-1', 'project notes', 0.8, ?)`,
		daysAgo(10))
	if err != nil {
		t.Fatal(err)
	}

	// Seed a recent obsidian entry (should not be touched).
	_, err = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, confidence, last_verified)
		 VALUES ('obs2', 'obsidian', 'vault-note-2', 'recent notes', 0.9, ?)`,
		nowStr())
	if err != nil {
		t.Fatal(err)
	}

	cleaned := pipe.Phase2_ObsidianVaultScan(ctx, store)
	// Should have decayed+deleted the stale entry.
	if cleaned < 1 {
		t.Errorf("expected at least 1 cleaned, got %d", cleaned)
	}

	// Verify stale entry was removed.
	var count int
	_ = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_index WHERE id = 'obs1'`).Scan(&count)
	if count != 0 {
		t.Errorf("stale obsidian entry should be deleted, found %d", count)
	}

	// Recent entry should remain.
	_ = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_index WHERE id = 'obs2'`).Scan(&count)
	if count != 1 {
		t.Errorf("recent obsidian entry should remain, found %d", count)
	}
}

// --- Phase 4: Tier State Sync (#57) ---

func TestPhase4_TierStateSync(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()

	// Create an episodic entry that has been pruned.
	_, err := store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance, memory_state)
		 VALUES ('ep-pruned', 'fact', 'pruned content', 3, 'pruned')`)
	if err != nil {
		t.Fatal(err)
	}

	// Create an index entry for it with high confidence.
	_, err = store.ExecContext(ctx,
		`INSERT INTO memory_index (id, source_table, source_id, summary, confidence)
		 VALUES ('mi-ep', 'episodic', 'ep-pruned', 'pruned content', 0.9)`)
	if err != nil {
		t.Fatal(err)
	}

	synced := pipe.Phase4_TierStateSync(ctx, store)
	if synced < 1 {
		t.Errorf("expected at least 1 synced, got %d", synced)
	}

	// Index confidence should be lowered.
	var conf float64
	_ = store.QueryRowContext(ctx, `SELECT confidence FROM memory_index WHERE id = 'mi-ep'`).Scan(&conf)
	if conf > 0.2 {
		t.Errorf("expected lowered confidence (<=0.1), got %f", conf)
	}
}

// --- Quiescence Gate (#58) ---

func TestQuiescenceGate_InitiallyNotQuiescent(t *testing.T) {
	gate := NewQuiescenceGate()
	if gate.IsQuiescent() {
		t.Error("gate should not be quiescent immediately after creation")
	}
}

func TestQuiescenceGate_BecomesQuiescent(t *testing.T) {
	gate := NewQuiescenceGateWithThreshold(10 * time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if !gate.IsQuiescent() {
		t.Error("gate should be quiescent after threshold elapses")
	}
}

func TestQuiescenceGate_ActivityResetsQuiescence(t *testing.T) {
	gate := NewQuiescenceGateWithThreshold(10 * time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if !gate.IsQuiescent() {
		t.Fatal("gate should be quiescent")
	}
	gate.RecordActivity()
	if gate.IsQuiescent() {
		t.Error("gate should not be quiescent after activity")
	}
}

func TestQuiescenceGate_TimeSinceActivity(t *testing.T) {
	gate := NewQuiescenceGate()
	time.Sleep(5 * time.Millisecond)
	elapsed := gate.TimeSinceActivity()
	if elapsed < 5*time.Millisecond {
		t.Errorf("expected at least 5ms elapsed, got %v", elapsed)
	}
}

// --- Confidence Decay Gate (#59) ---

func TestConfidenceDecayGate_FirstCallAllowed(t *testing.T) {
	gate := NewConfidenceDecayGate()
	if !gate.ShouldDecay() {
		t.Error("first call to ShouldDecay should return true")
	}
}

func TestConfidenceDecayGate_SecondCallBlocked(t *testing.T) {
	gate := NewConfidenceDecayGate()
	gate.ShouldDecay() // first call
	if gate.ShouldDecay() {
		t.Error("second immediate call to ShouldDecay should return false")
	}
}

func TestConfidenceDecayGate_AllowedAfterInterval(t *testing.T) {
	gate := NewConfidenceDecayGateWithInterval(10 * time.Millisecond)
	gate.ShouldDecay()
	time.Sleep(20 * time.Millisecond)
	if !gate.ShouldDecay() {
		t.Error("ShouldDecay should return true after interval elapses")
	}
}

func TestConfidenceDecayGate_Reset(t *testing.T) {
	gate := NewConfidenceDecayGate()
	gate.ShouldDecay()
	gate.Reset()
	if !gate.ShouldDecay() {
		t.Error("ShouldDecay should return true after Reset")
	}
}

func TestConfidenceDecayGate_LastRun(t *testing.T) {
	gate := NewConfidenceDecayGate()
	if !gate.LastRun().IsZero() {
		t.Error("LastRun should be zero before first decay")
	}
	gate.ShouldDecay()
	if gate.LastRun().IsZero() {
		t.Error("LastRun should not be zero after first decay")
	}
}

// --- Integration: full pipeline with new phases ---

func TestPipeline_FullRunWithNewPhases(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	pipe := NewConsolidationPipeline()
	pipe.MinInterval = 0

	// Seed derivable tool output.
	seedEpisodic(t, store, "ep-derive", "tool_event", "read_file: contents of main.go", 5, nowStr())
	// Seed within-tier duplicate (Jaccard >= 0.85 required for dedup).
	seedEpisodic(t, store, "ep-dup1", "fact", "the sun is shining brightly over the hills and valleys", 5, nowStr())
	seedEpisodic(t, store, "ep-dup2", "fact", "the sun is shining brightly over the hills and valleys today", 7, nowStr())
	// Seed low-confidence semantic.
	seedSemantic(t, store, "sem-low", "knowledge", "weak-fact", "barely known", 0.12, nowStr())

	r := pipe.Run(ctx, store)

	if r.DerivableStale < 1 {
		t.Errorf("expected at least 1 derivable stale, got %d", r.DerivableStale)
	}
	if r.Deduped < 1 {
		t.Errorf("expected at least 1 deduped, got %d", r.Deduped)
	}
	if r.Pruned < 1 {
		t.Errorf("expected at least 1 pruned, got %d", r.Pruned)
	}
}
