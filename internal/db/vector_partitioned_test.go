package db

import "testing"

func TestPartitionedIndex_RoutingBySourceTable(t *testing.T) {
	idx := NewPartitionedIndex(1)

	idx.AddEntry(VectorEntry{
		SourceTable: "episodic_memory", SourceID: "e1",
		Embedding: []float32{1, 0, 0},
	})
	idx.AddEntry(VectorEntry{
		SourceTable: "semantic_memory", SourceID: "s1",
		Embedding: []float32{0, 1, 0},
	})

	// Episodic → hot, semantic → warm. Total should be 2.
	if idx.EntryCount() != 2 {
		t.Errorf("total entry count = %d, want 2", idx.EntryCount())
	}

	// Verify routing: hot should have 1 (episodic), warm should have 1 (semantic).
	idx.mu.RLock()
	hotCount := idx.partitions["hot"].EntryCount()
	warmCount := idx.partitions["warm"].EntryCount()
	idx.mu.RUnlock()

	if hotCount != 1 {
		t.Errorf("hot partition count = %d, want 1", hotCount)
	}
	if warmCount != 1 {
		t.Errorf("warm partition count = %d, want 1", warmCount)
	}
}

func TestPartitionedIndex_UnknownTableDefaultsToWarm(t *testing.T) {
	idx := NewPartitionedIndex(1)

	idx.AddEntry(VectorEntry{
		SourceTable: "unknown_table", SourceID: "u1",
		Embedding: []float32{1, 0, 0},
	})

	idx.mu.RLock()
	warmCount := idx.partitions["warm"].EntryCount()
	idx.mu.RUnlock()

	if warmCount != 1 {
		t.Errorf("unknown table should route to warm, got warm count = %d", warmCount)
	}
}

func TestPartitionedIndex_SearchMerge(t *testing.T) {
	idx := NewPartitionedIndex(1)

	// Put similar vectors in different partitions.
	idx.AddEntry(VectorEntry{
		SourceTable: "episodic_memory", SourceID: "e1",
		ContentPreview: "hot entry",
		Embedding:      []float32{1, 0, 0},
	})
	idx.AddEntry(VectorEntry{
		SourceTable: "semantic_memory", SourceID: "s1",
		ContentPreview: "warm entry",
		Embedding:      []float32{0.9, 0.1, 0},
	})

	results := idx.Search([]float32{1, 0, 0}, 5)
	if len(results) != 2 {
		t.Fatalf("expected 2 results (merged from both partitions), got %d", len(results))
	}
	// The hot entry should rank first (exact match).
	if results[0].SourceID != "e1" {
		t.Errorf("expected e1 (exact match) first, got %s", results[0].SourceID)
	}
}

func TestPartitionedIndex_IsBuiltPartial(t *testing.T) {
	// Only warm has enough entries to be built.
	idx := NewPartitionedIndex(2)

	idx.AddEntry(VectorEntry{
		SourceTable: "episodic_memory", SourceID: "e1",
		Embedding: []float32{1, 0, 0},
	})
	// Hot has 1 entry (below threshold of 2), warm has 0.
	if idx.IsBuilt() {
		t.Error("should not be built when no partition reaches threshold")
	}

	// Add second episodic entry → hot reaches threshold.
	idx.AddEntry(VectorEntry{
		SourceTable: "episodic_memory", SourceID: "e2",
		Embedding: []float32{0, 1, 0},
	})
	if !idx.IsBuilt() {
		t.Error("should be built when at least one partition reaches threshold")
	}
}

func TestPartitionedIndex_SearchSkipsUnbuiltPartitions(t *testing.T) {
	// Threshold=100 means neither partition will be "built" with just a few entries.
	idx := NewPartitionedIndex(100)

	idx.AddEntry(VectorEntry{
		SourceTable: "episodic_memory", SourceID: "e1",
		Embedding: []float32{1, 0, 0},
	})

	// Neither partition is built, so Search should return nothing.
	results := idx.Search([]float32{1, 0, 0}, 5)
	if len(results) != 0 {
		t.Errorf("expected 0 results from unbuilt partitions, got %d", len(results))
	}
}

func TestPartitionedIndex_WorkingMemoryRoutesToHot(t *testing.T) {
	idx := NewPartitionedIndex(1)

	idx.AddEntry(VectorEntry{
		SourceTable: "working_memory", SourceID: "w1",
		Embedding: []float32{1, 0, 0},
	})

	idx.mu.RLock()
	hotCount := idx.partitions["hot"].EntryCount()
	idx.mu.RUnlock()

	if hotCount != 1 {
		t.Errorf("working_memory should route to hot, got hot count = %d", hotCount)
	}
}
